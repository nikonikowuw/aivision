<script lang="ts" setup>
import type { AlgorithmApi, TaskApi } from '#/api';

import { computed, nextTick, ref, watch } from 'vue';

import { IconifyIcon } from '@vben/icons';
import { $t } from '@vben/locales';

import {
  Alert,
  Button,
  Input,
  message,
  Modal,
  Radio,
  Slider,
  Switch,
  Tag,
} from 'ant-design-vue';

import { createInstanceApi, getAlgorithmList, updateInstanceApi } from '#/api';
import {
  formatAlgorithmDesc,
  formatAlgorithmName,
  formatTargetClass,
} from '#/utils/i18n';

import DetectionRuleEditor from './DetectionRuleEditor.vue';
import SchemaForm from './SchemaForm.vue';

interface Props {
  open?: boolean;
  cameraId: string;
  instance?: null | TaskApi.InstanceItem;
}

const props = withDefaults(defineProps<Props>(), {
  open: false,
  instance: null,
});

const emit = defineEmits<{
  (e: 'update:open', val: boolean): void;
  (e: 'success'): void;
}>();

const visible = computed({
  get: () => props.open,
  set: (val) => emit('update:open', val),
});

const isEdit = computed(() => Boolean(props.instance));
const title = computed(() =>
  isEdit.value
    ? $t('resource.task.instance.edit')
    : $t('resource.task.instance.add'),
);

const activeTab = ref<'params' | 'rules'>('params');
const loading = ref(false);
const submitting = ref(false);
const schemaFormRef = ref<InstanceType<typeof SchemaForm>>();

// 算法分类与检索
const selectedCategory = ref('all');
const algoSearchKeyword = ref('');

// 算法列表
const algorithmList = ref<AlgorithmApi.AlgorithmItem[]>([]);
const selectedAlgorithmId = ref<string>('');
const analysisFps = ref<number>(25);
const autoEnable = ref<boolean>(true);
const motionGateEnabled = ref<boolean>(true);
const motionGateKeepaliveMs = ref<number>(2000);
const similarityThreshold = ref<number>(0.7);
const paramsJson = ref<Record<string, unknown>>({});
const rules = ref<TaskApi.DetectionRule[]>([]);
const ruleEditorRef = ref<InstanceType<typeof DetectionRuleEditor>>();

function getAlgorithmCategory(algo: AlgorithmApi.AlgorithmItem): string {
  const id = algo.algorithmId.toLowerCase();
  const type = (algo.algorithmType || '').toLowerCase();
  if (id.includes('face') || type.includes('face')) return 'face';
  if (
    id.includes('fire') ||
    id.includes('smoke') ||
    id.includes('helmet') ||
    id.includes('safety')
  ) {
    return 'safety';
  }
  if (
    id.includes('plate') ||
    id.includes('traffic') ||
    id.includes('vehicle') ||
    id.includes('car')
  ) {
    return 'traffic';
  }
  return 'general';
}

function getCategoryInfo(algo: AlgorithmApi.AlgorithmItem) {
  const cat = getAlgorithmCategory(algo);
  switch (cat) {
    case 'face': {
      return {
        bg: 'bg-blue-500/10 text-blue-600 dark:text-blue-400',
        borderHover: 'hover:border-blue-500/40',
        categoryLabel: '人脸感知',
        icon: 'lucide:scan-face',
      };
    }
    case 'safety': {
      return {
        bg: 'bg-amber-500/10 text-amber-600 dark:text-amber-400',
        borderHover: 'hover:border-amber-500/40',
        categoryLabel: '安全防范',
        icon: 'lucide:shield-alert',
      };
    }
    case 'traffic': {
      return {
        bg: 'bg-cyan-500/10 text-cyan-600 dark:text-cyan-400',
        borderHover: 'hover:border-cyan-500/40',
        categoryLabel: '交通卡口',
        icon: 'lucide:car',
      };
    }
    default: {
      return {
        bg: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
        borderHover: 'hover:border-emerald-500/40',
        categoryLabel: '通用检测',
        icon: 'lucide:scan',
      };
    }
  }
}

const algoCategories = computed(() => {
  const list = algorithmList.value.filter((a) => Boolean(a.activeVersion));
  const counts: Record<string, number> = {
    all: list.length,
    face: 0,
    general: 0,
    safety: 0,
    traffic: 0,
  };
  for (const a of list) {
    const cat = getAlgorithmCategory(a);
    if (counts[cat] !== undefined) {
      counts[cat]++;
    }
  }
  return [
    { count: counts.all, key: 'all', label: '全部' },
    { count: counts.general, key: 'general', label: '通用检测' },
    { count: counts.face, key: 'face', label: '人脸感知' },
    { count: counts.safety, key: 'safety', label: '安全防范' },
    { count: counts.traffic, key: 'traffic', label: '交通卡口' },
  ];
});

const filteredAlgorithmList = computed(() => {
  const kw = algoSearchKeyword.value.trim().toLowerCase();
  return algorithmList.value.filter((a) => {
    if (!a.activeVersion) return false;
    if (selectedCategory.value !== 'all') {
      const cat = getAlgorithmCategory(a);
      if (cat !== selectedCategory.value) return false;
    }
    if (kw) {
      const matchName = a.name.toLowerCase().includes(kw);
      const matchId = a.algorithmId.toLowerCase().includes(kw);
      const matchDesc = (a.description || '').toLowerCase().includes(kw);
      if (!matchName && !matchId && !matchDesc) return false;
    }
    return true;
  });
});

// 当前选中的算法对象及其激活版本
const currentAlgorithm = computed(() => {
  return (
    algorithmList.value.find(
      (a) => a.algorithmId === selectedAlgorithmId.value,
    ) || null
  );
});

const currentCategoryInfo = computed(() => {
  if (!currentAlgorithm.value) {
    return {
      bg: 'bg-primary/10 text-primary',
      borderHover: 'hover:border-primary',
      categoryLabel: '推理模型',
      icon: 'lucide:cpu',
    };
  }
  return getCategoryInfo(currentAlgorithm.value);
});

const isFaceRecognition = computed(
  () => currentAlgorithm.value?.algorithmType === 'face_recognition',
);

const currentActiveVersion = computed(() => {
  if (!currentAlgorithm.value || !currentAlgorithm.value.activeVersion) {
    return null;
  }
  return (
    currentAlgorithm.value.versions?.find(
      (v) => v.version === currentAlgorithm.value?.activeVersion,
    ) || null
  );
});

// 计算当前激活版本的 FPS 档位信息
const fpsTiers = computed(() => {
  return currentActiveVersion.value?.fpsTiers || [];
});

const maxFps = computed(() => {
  if (fpsTiers.value.length === 0) return 30;
  const sorted = fpsTiers.value.toSorted((a, b) => b.fps - a.fps);
  return sorted[0]?.fps || 30;
});

const currentConfigSchema = computed(() => {
  return currentActiveVersion.value?.configSchema || null;
});

// 可选算法选项（仅保留具有激活版本的算法）
const algorithmOptions = computed(() => {
  return algorithmList.value
    .filter((a) => Boolean(a.activeVersion))
    .map((a) => ({
      label: `${formatAlgorithmName(a.algorithmId, a.name)} (${a.algorithmId})`,
      value: a.algorithmId,
    }));
});

// 当前生效的目标类别（供绘制区域时做明确指示）
const activeTargetClasses = computed<string[]>(() => {
  const val = paramsJson.value?.target_classes;
  if (Array.isArray(val)) {
    return val.map(String);
  }
  return [];
});

// 加载算法列表
async function loadAlgorithms() {
  loading.value = true;
  try {
    const res = await getAlgorithmList({ page: 1, pageSize: 100 });
    algorithmList.value = res.items || [];
  } catch {
    message.error($t('resource.task.instance.loadAlgoFailed'));
  } finally {
    loading.value = false;
  }
}

// 弹窗打开初始化
watch(
  () => props.open,
  async (isOpen) => {
    if (!isOpen) return;

    activeTab.value = 'params';
    algoSearchKeyword.value = '';
    selectedCategory.value = 'all';
    await loadAlgorithms();

    if (props.instance) {
      // 编辑回显
      selectedAlgorithmId.value = props.instance.algorithmId;
      analysisFps.value = props.instance.analysisFps || 25;
      rules.value = (props.instance.rules || []).map((rule) => ({
        lineDirection: rule.lineDirection,
        points: rule.points.map((point) => ({ x: point.x, y: point.y })),
        role: rule.role,
      }));

      if (typeof props.instance.paramsJson === 'string') {
        try {
          paramsJson.value = JSON.parse(props.instance.paramsJson || '{}');
        } catch {
          paramsJson.value = {};
        }
      } else {
        paramsJson.value = props.instance.paramsJson || {};
      }

      // 解析 motion_gate 配置回显
      const mg = props.instance.motionGate;
      if (mg && typeof mg === 'object') {
        motionGateEnabled.value = mg.enabled !== false;
        motionGateKeepaliveMs.value =
          typeof mg.keepaliveIntervalMs === 'number'
            ? mg.keepaliveIntervalMs
            : 2000;
      } else {
        motionGateEnabled.value = true;
        motionGateKeepaliveMs.value = 2000;
      }
      similarityThreshold.value = props.instance.similarityThreshold ?? 0.7;
    } else {
      // 新建默认
      analysisFps.value = 25;
      autoEnable.value = true;
      motionGateEnabled.value = true;
      motionGateKeepaliveMs.value = 2000;
      similarityThreshold.value = 0.7;
      paramsJson.value = {};
      rules.value = [];
      selectedAlgorithmId.value = algorithmOptions.value[0]?.value || '';
      await nextTick();
      schemaFormRef.value?.applyDefaults();
    }
  },
);

// 算法切换时，FPS 上限联动纠偏并重置新建状态下的参数
watch(selectedAlgorithmId, async (newId, oldId) => {
  if (!isEdit.value && maxFps.value && analysisFps.value > maxFps.value) {
    analysisFps.value = maxFps.value;
  }
  if (!isEdit.value && newId !== oldId) {
    paramsJson.value = {};
    await nextTick();
    schemaFormRef.value?.applyDefaults();
  }
});

async function handleOk() {
  if (!selectedAlgorithmId.value) {
    message.error($t('resource.task.instance.algorithmRequired'));
    return;
  }

  // 1. 校验 SchemaForm 动态参数
  try {
    await schemaFormRef.value?.validate();
  } catch {
    activeTab.value = 'params';
    message.error($t('resource.task.instance.paramsInvalid'));
    return;
  }

  // 2. 校验检测规则并复制为整份提交负载
  const rulesValid = ruleEditorRef.value?.validate() ?? true;
  if (!rulesValid) {
    activeTab.value = 'rules';
    message.error($t('resource.task.instance.rulesInvalid'));
    return;
  }
  const submittedRules = rules.value.map((rule) => ({
    lineDirection: rule.lineDirection,
    points: rule.points.map((point) => ({ x: point.x, y: point.y })),
    role: rule.role,
  }));

  const motionGatePayload: TaskApi.MotionGateConfig = {
    enabled: motionGateEnabled.value,
    keepaliveIntervalMs: motionGateKeepaliveMs.value || 2000,
  };

  submitting.value = true;
  try {
    if (isEdit.value && props.instance) {
      await updateInstanceApi(props.instance.instanceId, {
        analysisFps: analysisFps.value,
        paramsJson: paramsJson.value,
        rules: submittedRules,
        motionGate: motionGatePayload,
        similarityThreshold: isFaceRecognition.value
          ? similarityThreshold.value
          : undefined,
      });
      message.success($t('system.common.success'));
    } else {
      await createInstanceApi({
        cameraId: props.cameraId,
        algorithmId: selectedAlgorithmId.value,
        analysisFps: analysisFps.value,
        paramsJson: paramsJson.value,
        rules: submittedRules,
        motionGate: motionGatePayload,
        similarityThreshold: isFaceRecognition.value
          ? similarityThreshold.value
          : undefined,
        enabled: autoEnable.value,
      });
      message.success($t('system.common.success'));
    }

    emit('success');
    visible.value = false;
  } catch {
    // 拦截器已统一报错
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <Modal
    v-model:open="visible"
    :title="title"
    :confirm-loading="submitting"
    destroy-on-close
    width="min(1040px, 95vw)"
    @ok="handleOk"
  >
    <!-- 头部导航与 Tab 切换 -->
    <div
      class="flex flex-wrap items-center justify-between gap-2 pb-3 border-b border-border"
    >
      <div
        class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground"
      >
        <span>视频通道:</span>
        <span class="font-mono font-bold text-foreground">{{ cameraId }}</span>
        <Tag color="cyan" class="m-0 text-[10px]">硬件解码流水线</Tag>
      </div>

      <Radio.Group v-model:value="activeTab" button-style="solid" size="small">
        <Radio.Button value="params">
          <span class="flex items-center gap-1.5">
            <IconifyIcon icon="lucide:sliders" class="size-3.5" />
            <span>{{ $t('resource.task.instance.tabParams') }}</span>
          </span>
        </Radio.Button>
        <Radio.Button value="rules">
          <span class="flex items-center gap-1.5">
            <IconifyIcon icon="lucide:shapes" class="size-3.5" />
            <span>{{ $t('resource.task.instance.tabRules') }}</span>
            <Tag
              v-if="rules.length > 0"
              color="blue"
              class="m-0 text-[9px] px-1 py-0 ml-1"
            >
              {{ rules.length }}
            </Tag>
          </span>
        </Radio.Button>
      </Radio.Group>
    </div>

    <!-- Tab 1: 左右分栏算法集市与动态参数配置 -->
    <div
      v-show="activeTab === 'params'"
      class="mt-3.5 min-h-[500px] lg:h-[64vh] lg:max-h-[620px] grid grid-cols-12 gap-4"
    >
      <!-- 左侧 (小屏 12 列 / 大屏 5 列)：AI 算法模型货架 -->
      <div
        class="col-span-12 lg:col-span-5 border border-border rounded-xl bg-card flex flex-col overflow-hidden shadow-xs h-[360px] lg:h-full"
      >
        <!-- 门类筛选 Radio -->
        <div class="p-2 border-b border-border bg-muted/30 overflow-x-auto">
          <Radio.Group
            v-model:value="selectedCategory"
            button-style="solid"
            size="small"
            class="flex whitespace-nowrap min-w-max"
          >
            <Radio.Button
              v-for="cat in algoCategories"
              :key="cat.key"
              :value="cat.key"
              class="text-center text-xs px-2.5"
            >
              {{ cat.label }}
            </Radio.Button>
          </Radio.Group>
        </div>

        <!-- 搜索输入框 -->
        <div class="p-2 border-b border-border bg-card">
          <Input
            v-model:value="algoSearchKeyword"
            placeholder="搜索算法名称或 ID..."
            allow-clear
            size="small"
          >
            <template #prefix>
              <IconifyIcon
                icon="lucide:search"
                class="size-3.5 text-muted-foreground"
              />
            </template>
          </Input>
        </div>

        <!-- 算法卡片垂直滚动列表 -->
        <div class="flex-1 overflow-y-auto p-2.5 space-y-2">
          <div
            v-for="algo in filteredAlgorithmList"
            :key="algo.algorithmId"
            @click="!isEdit && (selectedAlgorithmId = algo.algorithmId)"
            class="rounded-xl border p-3 transition-all duration-200 flex flex-col justify-between group"
            :class="[
              selectedAlgorithmId === algo.algorithmId
                ? 'border-primary bg-primary/[0.04] ring-2 ring-primary/20 shadow-xs cursor-default'
                : isEdit
                  ? 'border-border/60 opacity-50 cursor-not-allowed'
                  : 'border-border bg-card hover:border-primary/50 cursor-pointer',
            ]"
          >
            <div>
              <div class="flex items-start justify-between gap-2 mb-1.5">
                <div class="flex items-center gap-2.5 min-w-0">
                  <div
                    class="flex size-8 shrink-0 items-center justify-center rounded-lg shadow-2xs"
                    :class="getCategoryInfo(algo).bg"
                  >
                    <IconifyIcon
                      :icon="getCategoryInfo(algo).icon"
                      class="size-4"
                    />
                  </div>
                  <div class="min-w-0">
                    <h4
                      class="text-xs font-semibold text-foreground truncate group-hover:text-primary transition-colors"
                    >
                      {{ formatAlgorithmName(algo.algorithmId, algo.name) }}
                    </h4>
                    <span class="text-[10px] text-muted-foreground">
                      {{ getCategoryInfo(algo).categoryLabel }}
                    </span>
                  </div>
                </div>

                <Tag
                  color="warning"
                  class="m-0 text-[10px] font-mono px-1.5 py-0 shrink-0"
                >
                  ⚡ 8 Units
                </Tag>
              </div>

              <p
                class="text-[11px] text-muted-foreground leading-relaxed line-clamp-2 mb-2"
              >
                {{
                  formatAlgorithmDesc(algo.algorithmId, algo.description) ||
                  '边缘神经网络推理算子，低延迟高并发。'
                }}
              </p>
            </div>

            <div
              class="pt-2 border-t border-border/60 flex items-center justify-between text-[10px]"
            >
              <div
                class="flex items-center gap-1.5 font-mono text-muted-foreground"
              >
                <Tag color="blue" class="m-0 text-[9px] px-1 py-0">
                  {{ algo.activeVersion || 'v1.0.0' }}
                </Tag>
                <span>·</span>
                <span class="text-emerald-600 dark:text-emerald-400">就绪</span>
              </div>

              <span
                v-if="selectedAlgorithmId === algo.algorithmId"
                class="text-primary font-medium text-xs flex items-center gap-1"
              >
                <IconifyIcon icon="lucide:check-circle" class="size-3.5" />
                <span>已选中</span>
              </span>
              <span
                v-else
                class="text-muted-foreground group-hover:text-foreground"
              >
                点击选择
              </span>
            </div>
          </div>

          <div
            v-if="filteredAlgorithmList.length === 0"
            class="py-12 text-center text-xs text-muted-foreground"
          >
            未找到匹配的算法模型
          </div>
        </div>
      </div>

      <!-- 右侧 (小屏 12 列 / 大屏 7 列)：选定算法专属参数配置面板 -->
      <div
        class="col-span-12 lg:col-span-7 border border-border rounded-xl bg-card flex flex-col justify-between overflow-hidden shadow-xs min-h-[420px] lg:h-full"
      >
        <!-- 头部：所选模型基本信息 -->
        <div
          class="p-3 px-4 border-b border-border bg-muted/25 flex items-center justify-between shrink-0"
        >
          <div class="flex items-center gap-2.5">
            <div
              class="flex size-8 shrink-0 items-center justify-center rounded-lg"
              :class="currentCategoryInfo.bg"
            >
              <IconifyIcon :icon="currentCategoryInfo.icon" class="size-4" />
            </div>
            <div>
              <div class="flex items-center gap-2">
                <h4 class="text-xs font-semibold text-foreground">
                  {{
                    formatAlgorithmName(
                      currentAlgorithm?.algorithmId,
                      currentAlgorithm?.name,
                    )
                  }}
                </h4>
                <Tag
                  v-if="currentAlgorithm?.activeVersion"
                  color="blue"
                  class="m-0 text-[10px] px-1 py-0"
                >
                  {{ currentAlgorithm.activeVersion }}
                </Tag>
              </div>
              <span class="text-[10px] text-muted-foreground">
                {{ currentCategoryInfo.categoryLabel }} · 推理参数配置
              </span>
            </div>
          </div>

          <Tag color="success" class="m-0 text-[10px] font-mono">
            算力申请: +8 Units
          </Tag>
        </div>

        <!-- 动态参数表单内容区 -->
        <div class="flex-1 overflow-y-auto p-4 space-y-4">
          <!-- 模块 1: 采样分析帧率 -->
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
              <span class="font-mono font-bold text-primary">{{ analysisFps }} FPS</span>
            </div>

            <Slider
              v-model:value="analysisFps"
              :min="1"
              :max="maxFps"
              :marks="{ 5: '5', 10: '10', 15: '15', 25: '25' }"
              class="my-2"
            />

            <div class="flex items-center gap-2 pt-2">
              <span class="text-xs text-muted-foreground font-mono">预设档位:</span>
              <Radio.Group
                v-model:value="analysisFps"
                size="small"
                button-style="solid"
              >
                <Radio.Button
                  v-for="preset in [5, 10, 15, 25].filter((p) => p <= maxFps)"
                  :key="preset"
                  :value="preset"
                >
                  {{ preset }}fps
                </Radio.Button>
              </Radio.Group>
            </div>
          </div>

          <!-- 模块 2: 模型专属动态参数 (SchemaForm 驱动) -->
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
                <span>模型推理定制参数</span>
              </span>
              <span class="font-mono text-[10px] text-muted-foreground">
                {{ selectedAlgorithmId }}
              </span>
            </div>

            <div class="pt-2">
              <SchemaForm
                ref="schemaFormRef"
                v-model:value="paramsJson"
                :schema="currentConfigSchema"
              />
            </div>
          </div>

          <!-- 模块 3: 人脸识别专属比对阈值 -->
          <div
            v-if="isFaceRecognition"
            class="rounded-xl border border-border bg-muted/20 p-3.5 space-y-2"
          >
            <div class="flex items-center justify-between text-xs">
              <span
                class="font-medium text-foreground flex items-center gap-1.5"
              >
                <IconifyIcon
                  icon="lucide:user-check"
                  class="size-3.5 text-primary"
                />
                <span>{{
                  $t('resource.task.instance.faceSimilarityThreshold')
                }}</span>
              </span>
              <span class="font-mono font-bold text-primary">{{
                similarityThreshold
              }}</span>
            </div>
            <Slider
              v-model:value="similarityThreshold"
              :min="0.1"
              :max="0.99"
              :step="0.01"
              class="my-1.5"
            />
            <p class="text-[11px] text-muted-foreground">
              {{ $t('resource.task.instance.faceSimilarityThresholdHint') }}
            </p>
          </div>

          <!-- 模块 4: 节能门控与调度使能 -->
          <div class="grid grid-cols-2 gap-3">
            <div
              class="p-3 rounded-xl border border-border bg-muted/20 flex items-center justify-between"
            >
              <div>
                <span class="text-xs font-medium text-foreground block">MotionGate™ 门控</span>
                <p class="text-[10px] text-muted-foreground mt-0.5">
                  静止画面休眠节能
                </p>
              </div>
              <Switch v-model:checked="motionGateEnabled" size="small" />
            </div>

            <div
              v-if="!isEdit"
              class="p-3 rounded-xl border border-border bg-muted/20 flex items-center justify-between"
            >
              <div>
                <span class="text-xs font-medium text-foreground block">创建即调度</span>
                <p class="text-[10px] text-muted-foreground mt-0.5">
                  保存后立即投入运行
                </p>
              </div>
              <Switch v-model:checked="autoEnable" size="small" />
            </div>
          </div>

          <!-- 模块 5: 算力预检状态 -->
          <Alert
            type="info"
            show-icon
            message="边缘算力预检通过"
            description="本次申请 8 Units，调度后算力负荷充足，无需排队。"
          />
        </div>
      </div>
    </div>

    <!-- Tab 2: 区域与警戒线绘制 (DetectionRuleEditor) -->
    <div v-show="activeTab === 'rules'" class="pt-3 space-y-2">
      <div class="flex items-center justify-between">
        <div class="flex items-center gap-2 text-xs text-muted-foreground">
          <span>当前生效目标:</span>
          <span
            v-if="activeTargetClasses.length > 0"
            class="flex items-center gap-1 font-mono text-foreground font-medium"
          >
            <Tag
              v-for="cls in activeTargetClasses"
              :key="cls"
              color="purple"
              class="m-0 text-[10px]"
            >
              {{ formatTargetClass(cls) }}
            </Tag>
          </span>
          <span v-else class="text-muted-foreground italic text-[11px]">
            全量目标 (全屏巡检)
          </span>
        </div>
        <Button
          size="small"
          type="link"
          class="text-xs p-0 h-auto"
          @click="activeTab = 'params'"
        >
          {{ $t('resource.task.instance.adjustTargets') }} →
        </Button>
      </div>

      <div class="rounded-xl border border-border bg-card p-3 shadow-2xs">
        <DetectionRuleEditor
          ref="ruleEditorRef"
          v-model:value="rules"
          :camera-id="props.cameraId"
          :open="visible && activeTab === 'rules'"
        />
      </div>
    </div>

    <!-- 弹窗底部操作栏 -->
    <template #footer>
      <div class="flex items-center justify-between">
        <div>
          <Button v-if="activeTab === 'rules'" @click="activeTab = 'params'">
            ← {{ $t('resource.task.instance.tabParams') }}
          </Button>
          <Button v-else type="dashed" @click="activeTab = 'rules'">
            {{ $t('resource.task.instance.tabRules') }} →
          </Button>
        </div>
        <div class="flex items-center gap-2">
          <Button @click="visible = false">
            {{ $t('system.common.cancel') }}
          </Button>
          <Button type="primary" :loading="submitting" @click="handleOk">
            {{ $t('system.common.confirm') }}
          </Button>
        </div>
      </div>
    </template>
  </Modal>
</template>
