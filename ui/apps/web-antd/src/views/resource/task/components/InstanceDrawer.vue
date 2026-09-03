<script lang="ts" setup>
import type { AlgorithmApi, TaskApi } from '#/api';

import { computed, onUnmounted, ref, watch } from 'vue';

import { IconifyIcon } from '@vben/icons';
import { $t } from '@vben/locales';

import {
  Button,
  Drawer,
  Empty,
  message,
  Popconfirm,
  Progress,
  Radio,
  Slider,
  Spin,
  Switch,
  Tag,
} from 'ant-design-vue';

import {
  deleteInstanceApi,
  getAlgorithmList,
  getInstanceListApi,
  setInstanceEnabledApi,
  updateInstanceApi,
} from '#/api';
import { formatAlgorithmName } from '#/utils/i18n';

import DetectionRuleEditor from './DetectionRuleEditor.vue';
import InstanceFormModal from './InstanceFormModal.vue';
import SchemaForm from './SchemaForm.vue';

interface Props {
  open?: boolean;
  cameraId?: string;
  taskName?: string;
}

const props = withDefaults(defineProps<Props>(), {
  open: false,
  cameraId: '',
  taskName: '',
});

const emit = defineEmits<{
  (e: 'update:open', val: boolean): void;
  (e: 'change'): void;
}>();

const visible = computed({
  get: () => props.open,
  set: (val) => emit('update:open', val),
});

const loading = ref(false);
const savingActive = ref(false);
const instances = ref<TaskApi.InstanceItem[]>([]);
const algorithmList = ref<AlgorithmApi.AlgorithmItem[]>([]);

// 当前工作台选中的算法实例 ID
const activeInstanceId = ref('');

// 右侧检查器就地参数编辑状态
const activeFps = ref(15);
const activeMotionGate = ref(true);
const activeParams = ref<Record<string, unknown>>({});
const activeRules = ref<TaskApi.DetectionRule[]>([]);

// 表单与编辑器 Ref
const schemaFormRef = ref<InstanceType<typeof SchemaForm> | null>(null);
const ruleEditorRef = ref<InstanceType<typeof DetectionRuleEditor> | null>(
  null,
);

// 实例新增/编辑完整弹窗 (集市)
const formModalOpen = ref(false);
const currentEditInstance = ref<null | TaskApi.InstanceItem>(null);

// 轮询控制
let pollTimer: null | ReturnType<typeof setInterval> = null;
let pollStartTime = 0;
let isFetchingInstances = false;
const POLL_INTERVAL_MS = 2000;
const MAX_POLL_DURATION_MS = 15_000;

function hasActiveInstances(items: TaskApi.InstanceItem[]): boolean {
  // 仅在存在过渡态 (1: 调度启动中) 实例时进行短时状态收敛轮询；稳定态 (2: 运行中 / 4: 已停止) 绝不轮询，杜绝无谓的重绘与界面闪烁
  return items.some((inst) => inst.actualStatus === 1);
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

function startPolling() {
  if (pollTimer) return;
  pollStartTime = Date.now();
  pollTimer = setInterval(async () => {
    if (Date.now() - pollStartTime > MAX_POLL_DURATION_MS) {
      stopPolling();
      return;
    }
    await loadInstancesSilently();
  }, POLL_INTERVAL_MS);
}

async function loadInstancesSilently() {
  if (!props.cameraId || isFetchingInstances) return;
  isFetchingInstances = true;
  try {
    const list = await getInstanceListApi(props.cameraId);
    if (!list) return;

    if (instances.value.length === 0) {
      instances.value = list;
      if (list.length > 0 && !activeInstanceId.value && list[0]) {
        selectInstance(list[0]);
      }
      return;
    }

    // 原位局部更新，严禁破坏数组与对象引用，避免引发全树抖动与高频重渲染
    const freshMap = new Map(list.map((i) => [i.instanceId, i]));
    for (const inst of instances.value) {
      const fresh = freshMap.get(inst.instanceId);
      if (fresh) {
        inst.actualStatus = fresh.actualStatus;
        inst.currentFps = fresh.currentFps;
        inst.analysisFps = fresh.analysisFps;
        inst.enabled = fresh.enabled;
        inst.rules = fresh.rules;
        inst.statusMessage = fresh.statusMessage;
      }
    }

    if (list.length !== instances.value.length) {
      instances.value = list;
      const currentExists = instances.value.some(
        (i) => i.instanceId === activeInstanceId.value,
      );
      if (!currentExists && instances.value.length > 0 && instances.value[0]) {
        selectInstance(instances.value[0]);
      }
    }

    if (!hasActiveInstances(instances.value)) {
      stopPolling();
    }
  } catch {
    // 静默容错
  } finally {
    isFetchingInstances = false;
  }
}

async function initializeDrawer() {
  if (!props.cameraId) return;
  loading.value = true;
  stopPolling();
  try {
    const [algoRes, instanceList] = await Promise.all([
      getAlgorithmList({ page: 1, pageSize: 100 }),
      getInstanceListApi(props.cameraId),
    ]);
    algorithmList.value = algoRes.items || [];
    instances.value = instanceList || [];

    if (instances.value.length > 0) {
      const current = instances.value.find(
        (i) => i.instanceId === activeInstanceId.value,
      );
      const target = current || instances.value[0];
      if (target) {
        selectInstance(target);
      }
    } else {
      activeInstanceId.value = '';
    }

    if (hasActiveInstances(instances.value)) {
      startPolling();
    }
  } catch {
    message.error($t('resource.task.instance.loadFailed'));
  } finally {
    loading.value = false;
  }
}

const activeInstance = computed(() => {
  return (
    instances.value.find((i) => i.instanceId === activeInstanceId.value) || null
  );
});

const activeAlgorithm = computed(() => {
  if (!activeInstance.value) return null;
  return (
    algorithmList.value.find(
      (a) => a.algorithmId === activeInstance.value?.algorithmId,
    ) || null
  );
});

const schemaCache = new Map<string, Record<string, unknown>>();

const activeAlgorithmSchema = computed<null | Record<string, unknown>>(() => {
  if (!activeAlgorithm.value) return null;
  const algoId = activeAlgorithm.value.algorithmId;
  const verStr = activeAlgorithm.value.activeVersion || 'default';
  const cacheKey = `${algoId}:${verStr}`;
  const cached = schemaCache.get(cacheKey);
  if (cached) {
    return cached;
  }

  let raw: unknown = null;
  if (activeAlgorithm.value.activeVersion) {
    const ver = activeAlgorithm.value.versions?.find(
      (v) => v.version === activeAlgorithm.value?.activeVersion,
    );
    raw = ver?.configSchema;
  }
  if (!raw && activeAlgorithm.value.versions?.length) {
    raw = activeAlgorithm.value.versions[0]?.configSchema;
  }
  if (typeof raw === 'string') {
    try {
      raw = JSON.parse(raw);
    } catch {
      return null;
    }
  }
  if (raw && typeof raw === 'object' && !Array.isArray(raw)) {
    const res = raw as Record<string, unknown>;
    schemaCache.set(cacheKey, res);
    return res;
  }
  return null;
});

function selectInstance(inst: TaskApi.InstanceItem) {
  activeInstanceId.value = inst.instanceId;
  activeFps.value = inst.analysisFps || 15;
  activeMotionGate.value = inst.motionGate?.enabled !== false;
  activeRules.value = (inst.rules || []).map((r) => ({
    lineDirection: r.lineDirection,
    points: r.points.map((p) => ({ x: p.x, y: p.y })),
    role: r.role,
  }));

  let parsed: Record<string, unknown> = {};
  if (typeof inst.paramsJson === 'string') {
    try {
      parsed = JSON.parse(inst.paramsJson || '{}');
    } catch {
      parsed = {};
    }
  } else if (inst.paramsJson && typeof inst.paramsJson === 'object') {
    parsed = { ...inst.paramsJson };
  }
  activeParams.value = parsed;
}

async function handleSaveActive() {
  if (!activeInstance.value) return;

  if (schemaFormRef.value) {
    try {
      const valid = await schemaFormRef.value.validate();
      if (!valid) return;
    } catch {
      message.error('请检查参数配置项是否完整有效');
      return;
    }
  }

  if (ruleEditorRef.value) {
    const valid = ruleEditorRef.value.validate();
    if (!valid) {
      message.error('防区规则存在未闭合或自相交错误，请检查中栏画布');
      return;
    }
  }

  savingActive.value = true;
  try {
    await updateInstanceApi(activeInstance.value.instanceId, {
      analysisFps: activeFps.value,
      paramsJson: activeParams.value,
      rules: activeRules.value,
      motionGate: {
        enabled: activeMotionGate.value,
        keepaliveIntervalMs: 2000,
      },
    });
    message.success($t('system.common.success'));
    emit('change');
    await loadInstancesSilently();
  } catch {
    // 拦截器已统一报错
  } finally {
    savingActive.value = false;
  }
}

function handleVisibilityChange() {
  if (document.hidden) {
    stopPolling();
  } else if (props.open && props.cameraId) {
    void loadInstancesSilently();
  }
}

watch(
  () => props.open,
  (isOpen) => {
    if (isOpen && props.cameraId) {
      document.addEventListener('visibilitychange', handleVisibilityChange);
      void initializeDrawer();
    } else {
      document.removeEventListener('visibilitychange', handleVisibilityChange);
      stopPolling();
      activeInstanceId.value = '';
    }
  },
);

onUnmounted(() => {
  document.removeEventListener('visibilitychange', handleVisibilityChange);
  stopPolling();
});

function handleAdd() {
  currentEditInstance.value = null;
  formModalOpen.value = true;
}

function handleEdit(inst: TaskApi.InstanceItem) {
  currentEditInstance.value = inst;
  formModalOpen.value = true;
}

async function handleDelete(inst: TaskApi.InstanceItem) {
  try {
    await deleteInstanceApi(inst.instanceId);
    message.success($t('system.common.success'));
    emit('change');
    await initializeDrawer();
  } catch {
    // 拦截器已统一报错
  }
}

async function handleToggleEnabled(
  inst: TaskApi.InstanceItem,
  checked: boolean,
) {
  inst.enabled = checked;
  try {
    await setInstanceEnabledApi(inst.instanceId, checked);
    message.success($t('system.common.success'));
    emit('change');
    startPolling();
  } catch {
    inst.enabled = !checked;
  }
}

async function handleFormSuccess() {
  formModalOpen.value = false;
  emit('change');
  await initializeDrawer();
}

function getStatusTag(status: number) {
  switch (status) {
    case 1: {
      return { color: 'processing', text: '调度中' };
    }
    case 2: {
      return { color: 'success', text: '分析中' };
    }
    case 3: {
      return { color: 'warning', text: '降级' };
    }
    case 4: {
      return { color: 'default', text: '已停止' };
    }
    case 5: {
      return { color: 'error', text: '异常' };
    }
    default: {
      return { color: 'default', text: '就绪' };
    }
  }
}
</script>

<template>
  <Drawer
    v-model:open="visible"
    width="92vw"
    :body-style="{ padding: '16px', overflowY: 'auto' }"
    destroy-on-close
  >
    <template #title>
      <div
        class="flex flex-wrap items-center justify-between gap-3 w-full pr-6"
      >
        <div class="flex items-center gap-3 min-w-0">
          <div
            class="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary"
          >
            <IconifyIcon icon="lucide:video" class="size-4" />
          </div>
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-1.5 sm:gap-2">
              <span
                class="text-sm font-bold text-foreground truncate max-w-[140px] sm:max-w-none"
              >
                {{ taskName || cameraId }}
              </span>
              <span
                class="font-mono text-xs text-muted-foreground bg-muted px-1.5 py-0.5 rounded shrink-0"
              >
                {{ cameraId }}
              </span>
              <Tag color="cyan" class="m-0 text-[10px] shrink-0">
                1080P@25fps
              </Tag>
              <Tag color="blue" class="m-0 text-[10px] shrink-0">
                {{ instances.length }} 算法实例
              </Tag>
            </div>
          </div>
        </div>

        <div class="flex items-center gap-2 sm:gap-3 shrink-0">
          <Tag color="warning" class="m-0 text-xs font-mono px-2 py-0.5">
            算力调度: {{ instances.length * 8 }} Units
          </Tag>
          <Button
            type="primary"
            size="small"
            class="flex items-center gap-1.5 shadow-xs bg-emerald-600 hover:bg-emerald-500 border-none"
            @click="handleAdd"
          >
            <IconifyIcon icon="lucide:plus" class="size-3.5" />
            <span>挂载算法模型</span>
          </Button>
        </div>
      </div>
    </template>

    <Spin :spinning="loading && instances.length === 0">
      <!-- 三栏响应式 IDE 工作台架构 (Three-Column Studio Layout) -->
      <div
        class="grid grid-cols-12 gap-4 xl:h-[calc(100vh-125px)] min-h-[620px]"
      >
        <!-- 左栏 (小屏 12 列 / 中屏 4 列 / 大屏 3 列)：垂直算法流水线资产树 -->
        <div
          class="col-span-12 lg:col-span-4 xl:col-span-3 border border-border rounded-xl bg-card flex flex-col overflow-hidden shadow-xs h-[360px] lg:h-[560px] xl:h-full"
        >
          <div
            class="p-3 border-b border-border bg-muted/20 flex items-center justify-between shrink-0"
          >
            <span
              class="text-xs font-semibold text-foreground flex items-center gap-1.5"
            >
              <IconifyIcon icon="lucide:layers" class="size-3.5 text-primary" />
              <span>算法流水线 ({{ instances.length }})</span>
            </span>
            <Button
              type="link"
              size="small"
              class="p-0 text-xs text-primary"
              @click="handleAdd"
            >
              + 挂载
            </Button>
          </div>

          <!-- 算法实例列表 -->
          <div class="flex-1 overflow-y-auto p-2.5 space-y-2">
            <div
              v-for="inst in instances"
              :key="inst.instanceId"
              class="rounded-xl border p-3 transition-all duration-150 flex flex-col justify-between cursor-pointer group"
              :class="[
                activeInstanceId === inst.instanceId
                  ? 'border-primary bg-primary/[0.04] ring-1 ring-primary/30 shadow-xs'
                  : 'border-border bg-card hover:border-primary/40',
              ]"
              @click="selectInstance(inst)"
            >
              <!-- 头部：名称、版本、使能开关 -->
              <div class="flex items-start justify-between gap-1 mb-2">
                <div class="flex items-center gap-2 min-w-0">
                  <span
                    class="size-2 rounded-full shrink-0"
                    :class="
                      inst.enabled ? 'bg-emerald-500' : 'bg-muted-foreground/40'
                    "
                  ></span>
                  <div class="min-w-0">
                    <h4
                      class="text-xs font-semibold text-foreground truncate group-hover:text-primary transition-colors"
                    >
                      {{ formatAlgorithmName(inst.algorithmId) }}
                    </h4>
                    <span class="text-[10px] font-mono text-muted-foreground">
                      {{ inst.algorithmId }}
                    </span>
                  </div>
                </div>

                <div class="flex items-center gap-1 shrink-0" @click.stop>
                  <Switch
                    :checked="inst.enabled"
                    size="small"
                    @change="(val) => handleToggleEnabled(inst, Boolean(val))"
                  />
                </div>
              </div>

              <!-- 吞吐能量进度条 -->
              <div class="space-y-1 mb-2 bg-muted/30 p-2 rounded-lg">
                <div
                  class="flex flex-wrap justify-between items-center gap-1 text-[10px] font-mono text-muted-foreground"
                >
                  <span>采样吞吐</span>
                  <span class="whitespace-nowrap">
                    <strong class="text-foreground">{{
                      (inst.currentFps ?? 0).toFixed(1)
                    }}</strong>
                    / {{ inst.analysisFps }} fps
                  </span>
                </div>
                <Progress
                  :percent="
                    inst.analysisFps > 0
                      ? Math.min(
                          100,
                          Math.round(
                            ((inst.currentFps ?? 0) / inst.analysisFps) * 100,
                          ),
                        )
                      : 0
                  "
                  :show-info="false"
                  size="small"
                  :stroke-color="inst.enabled ? '#10b981' : '#94a3b8'"
                />
              </div>

              <!-- 底部操作与状态 -->
              <div
                class="pt-2 border-t border-border/60 flex items-center justify-between text-[10px]"
              >
                <Tag
                  :color="getStatusTag(inst.actualStatus).color"
                  class="m-0 text-[9px] px-1 py-0"
                >
                  {{ getStatusTag(inst.actualStatus).text }}
                </Tag>

                <div class="flex items-center gap-1" @click.stop>
                  <Button
                    type="text"
                    size="small"
                    class="p-0 size-6 text-muted-foreground hover:text-foreground"
                    title="完整配置"
                    @click="handleEdit(inst)"
                  >
                    <IconifyIcon icon="lucide:settings" class="size-3.5" />
                  </Button>

                  <Popconfirm
                    :title="$t('resource.task.instance.deleteConfirm')"
                    @confirm="handleDelete(inst)"
                  >
                    <Button
                      type="text"
                      size="small"
                      class="p-0 size-6 text-muted-foreground hover:text-rose-500"
                      title="移除"
                    >
                      <IconifyIcon icon="lucide:trash-2" class="size-3.5" />
                    </Button>
                  </Popconfirm>
                </div>
              </div>
            </div>

            <!-- 空状态添加引导 -->
            <div
              v-if="instances.length === 0"
              class="py-12 text-center text-xs text-muted-foreground space-y-2"
            >
              <p>暂无挂载算法实例</p>
              <Button size="small" type="dashed" @click="handleAdd">
                ＋ 立即挂载首个模型
              </Button>
            </div>

            <!-- 底部常驻挂载插槽 -->
            <Button
              v-if="instances.length > 0"
              type="dashed"
              block
              size="small"
              class="mt-1 text-xs"
              @click="handleAdd"
            >
              <IconifyIcon icon="lucide:plus" class="size-3.5" />
              <span>挂载新算法模型</span>
            </Button>
          </div>
        </div>

        <!-- 中栏 (小屏 12 列 / 中屏 8 列 / 大屏 5 列)：视频流与几何规则画布 -->
        <div
          class="col-span-12 lg:col-span-8 xl:col-span-5 border border-border rounded-xl bg-card flex flex-col overflow-hidden shadow-xs min-h-[500px] lg:h-[560px] xl:h-full"
        >
          <div
            class="p-2.5 px-4 border-b border-border bg-muted/20 flex items-center justify-between shrink-0"
          >
            <div class="flex items-center gap-2">
              <span
                class="size-2 rounded-full bg-emerald-500 shadow-[0_0_6px_rgba(16,185,129,0.8)]"
              ></span>
              <span class="text-xs font-semibold text-foreground">
                实时视频流与防区几何规则
              </span>
            </div>
            <Tag color="blue" class="m-0 text-[10px]">
              {{
                activeInstance
                  ? formatAlgorithmName(activeInstance.algorithmId)
                  : '未选中算法'
              }}
            </Tag>
          </div>

          <!-- 视频与多边形规则画布嵌入区 (内部必须可纵向滚动以完整展示规则列表与画布) -->
          <div class="flex-1 overflow-y-auto p-3 bg-muted/10">
            <div v-if="activeInstance">
              <DetectionRuleEditor
                ref="ruleEditorRef"
                v-model:value="activeRules"
                :camera-id="props.cameraId"
                :open="props.open"
              />
            </div>
            <div
              v-else
              class="h-64 flex flex-col items-center justify-center text-muted-foreground space-y-2"
            >
              <IconifyIcon
                icon="lucide:monitor-play"
                class="size-8 opacity-40 mx-auto"
              />
              <p class="text-xs">请在左侧选择一个算法模型进行防区调试</p>
            </div>
          </div>
        </div>

        <!-- 右栏 (小屏 12 列 / 中屏 12 列 / 大屏 4 列)：即时属性检查器 -->
        <div
          class="col-span-12 lg:col-span-12 xl:col-span-4 border border-border rounded-xl bg-card flex flex-col justify-between overflow-hidden shadow-xs min-h-[460px] xl:h-full"
        >
          <div
            class="p-3 px-4 border-b border-border bg-muted/20 flex items-center justify-between shrink-0"
          >
            <div class="flex items-center gap-2">
              <IconifyIcon
                icon="lucide:sliders"
                class="size-3.5 text-primary"
              />
              <h4 class="text-xs font-semibold text-foreground truncate">
                {{
                  activeInstance
                    ? formatAlgorithmName(activeInstance.algorithmId)
                    : '参数检查器'
                }}
              </h4>
            </div>
            <Tag
              v-if="activeInstance"
              color="processing"
              class="m-0 text-[10px]"
            >
              就绪
            </Tag>
          </div>

          <!-- 调参滚动内容区 -->
          <div
            v-if="activeInstance"
            class="flex-1 overflow-y-auto p-4 space-y-4"
          >
            <!-- 采样帧率 (FPS) -->
            <div
              class="rounded-xl border border-border bg-muted/20 p-3.5 space-y-2"
            >
              <div class="flex items-center justify-between text-xs">
                <span
                  class="font-medium text-foreground flex items-center gap-1.5"
                >
                  <IconifyIcon
                    icon="lucide:gauge"
                    class="size-3.5 text-primary"
                  />
                  <span>{{ $t('resource.task.instance.analysisFps') }}</span>
                </span>
                <span class="font-mono font-bold text-primary">{{ activeFps }} FPS</span>
              </div>

              <Slider
                v-model:value="activeFps"
                :min="1"
                :max="30"
                :marks="{ 5: '5', 10: '10', 15: '15', 25: '25' }"
                class="my-2"
              />

              <div class="flex items-center gap-2 pt-2">
                <span class="text-xs text-muted-foreground font-mono">预设:</span>
                <Radio.Group
                  v-model:value="activeFps"
                  size="small"
                  button-style="solid"
                >
                  <Radio.Button
                    v-for="preset in [5, 10, 15, 25]"
                    :key="preset"
                    :value="preset"
                  >
                    {{ preset }}fps
                  </Radio.Button>
                </Radio.Group>
              </div>
            </div>

            <!-- 模型专属动态 Schema 表单 -->
            <div
              class="rounded-xl border border-border bg-muted/20 p-3.5 space-y-2"
            >
              <div
                class="flex items-center justify-between pb-2 border-b border-border text-xs"
              >
                <span
                  class="font-medium text-foreground flex items-center gap-1.5"
                >
                  <IconifyIcon
                    icon="lucide:sliders-horizontal"
                    class="size-3.5 text-primary"
                  />
                  <span>专属推理参数 (Schema)</span>
                </span>
                <span class="font-mono text-[10px] text-muted-foreground">
                  {{ activeInstance.algorithmId }}
                </span>
              </div>

              <div class="pt-2">
                <SchemaForm
                  :key="activeInstance.instanceId"
                  ref="schemaFormRef"
                  v-model:value="activeParams"
                  :schema="activeAlgorithmSchema"
                />
              </div>
            </div>

            <!-- 节能门控 Switch -->
            <div
              class="p-3 rounded-xl border border-border bg-muted/20 flex items-center justify-between"
            >
              <div>
                <span class="text-xs font-medium text-foreground block">MotionGate™ 门控</span>
                <p class="text-[10px] text-muted-foreground mt-0.5">
                  静止画面休眠节能
                </p>
              </div>
              <Switch v-model:checked="activeMotionGate" size="small" />
            </div>
          </div>

          <div v-else class="flex-1 flex items-center justify-center p-4">
            <Empty description="请先在左侧选择一个算法模型" />
          </div>

          <!-- 右侧底部保存操作栏 -->
          <div
            v-if="activeInstance"
            class="p-3 px-4 border-t border-border bg-muted/20 flex items-center justify-between shrink-0"
          >
            <span class="text-xs text-muted-foreground font-mono">
              修改即刻生效
            </span>
            <Button
              type="primary"
              size="small"
              :loading="savingActive"
              class="px-5 shadow-xs bg-emerald-600 hover:bg-emerald-500 border-none"
              @click="handleSaveActive"
            >
              保存并生效
            </Button>
          </div>
        </div>
      </div>
    </Spin>

    <!-- 算法模型完整新增/编辑弹窗 (左右分栏算法集市) -->
    <InstanceFormModal
      v-model:open="formModalOpen"
      :camera-id="props.cameraId"
      :instance="currentEditInstance"
      @success="handleFormSuccess"
    />
  </Drawer>
</template>
