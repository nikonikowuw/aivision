<script lang="ts" setup>
import type { AlgorithmApi, TaskApi } from '#/api';

import { computed, nextTick, ref, watch } from 'vue';

import { $t } from '@vben/locales';

import {
  Button,
  Form,
  FormItem,
  InputNumber,
  message,
  Modal,
  Select,
  Switch,
  TabPane,
  Tabs,
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

// 算法列表
const algorithmList = ref<AlgorithmApi.AlgorithmItem[]>([]);
const selectedAlgorithmId = ref<string>('');
const analysisFps = ref<number>(25);
const autoEnable = ref<boolean>(true);
const motionGateEnabled = ref<boolean>(true);
const motionGateKeepaliveMs = ref<number>(2000);
const paramsJson = ref<Record<string, unknown>>({});
const rules = ref<TaskApi.DetectionRule[]>([]);
const ruleEditorRef = ref<InstanceType<typeof DetectionRuleEditor>>();

// 当前选中的算法对象及其激活版本
const currentAlgorithm = computed(() => {
  return (
    algorithmList.value.find(
      (a) => a.algorithmId === selectedAlgorithmId.value,
    ) || null
  );
});

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

const fpsTiersDisplay = computed(() => {
  if (fpsTiers.value.length === 0)
    return $t('resource.task.instance.noTierConstraint');
  return fpsTiers.value
    .map(
      (t) =>
        `${t.fps}fps (${t.units} ${$t('resource.task.instance.tierUnit')})`,
    )
    .join(', ');
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
    } else {
      // 新建默认
      analysisFps.value = 25;
      autoEnable.value = true;
      motionGateEnabled.value = true;
      motionGateKeepaliveMs.value = 2000;
      paramsJson.value = {};
      rules.value = [];
      // 保证每次新建时都重新选择默认算法并触发 schema 变化
      selectedAlgorithmId.value = algorithmOptions.value[0]?.value || '';
      // 显式应用算法 schema 默认值：重新打开弹窗时 schema 引用可能不变，
      // SchemaForm 的 watcher 不会再次触发，必须主动补齐默认值。
      await nextTick();
      schemaFormRef.value?.applyDefaults();
    }
  },
);

// 算法切换时，FPS 上限联动纠偏并重置新建状态下的参数
watch(selectedAlgorithmId, (newId, oldId) => {
  if (!isEdit.value && maxFps.value && analysisFps.value > maxFps.value) {
    analysisFps.value = maxFps.value;
  }
  if (!isEdit.value && newId !== oldId) {
    paramsJson.value = {};
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
    width="1040px"
    @ok="handleOk"
  >
    <Tabs v-model:active-key="activeTab" class="instance-form-tabs mt-1">
      <!-- Tab 1: 算法与参数配置 -->
      <TabPane key="params">
        <template #tab>
          <span class="flex items-center gap-1.5 px-1 font-medium">
            <span>⚙️</span>
            <span>{{ $t('resource.task.instance.tabParams') }}</span>
          </span>
        </template>

        <Form layout="vertical" class="pt-2 max-h-[62vh] overflow-y-auto pr-1">
          <!-- 选择算法 -->
          <FormItem
            :label="$t('resource.task.instance.algorithm')"
            required
            class="mb-3"
          >
            <Select
              v-model:value="selectedAlgorithmId"
              :options="algorithmOptions"
              :disabled="isEdit"
              :placeholder="
                $t('resource.task.instance.algorithmSelectPlaceholder')
              "
              class="w-full"
            />
            <div
              v-if="currentAlgorithm"
              class="text-muted-foreground mt-1.5 flex flex-col gap-1 text-xs"
            >
              <div class="flex items-center gap-2">
                <span>{{ $t('resource.task.instance.activeVersion') }}:</span>
                <Tag v-if="currentAlgorithm.activeVersion" color="blue">
                  {{ currentAlgorithm.activeVersion }}
                </Tag>
                <Tag v-else color="red">
                  {{ $t('resource.task.instance.noActiveVersion') }}
                </Tag>
              </div>
              <div
                v-if="
                  formatAlgorithmDesc(
                    currentAlgorithm.algorithmId,
                    currentAlgorithm.description,
                  )
                "
                class="text-xs text-muted-foreground"
              >
                {{
                  formatAlgorithmDesc(
                    currentAlgorithm.algorithmId,
                    currentAlgorithm.description,
                  )
                }}
              </div>
            </div>
          </FormItem>

          <!-- 采样帧率 (FPS) -->
          <FormItem
            :label="$t('resource.task.instance.analysisFps')"
            required
            class="mb-3"
          >
            <InputNumber
              v-model:value="analysisFps"
              :min="1"
              :max="maxFps"
              :precision="0"
              class="w-full"
              :placeholder="$t('resource.task.instance.analysisFpsRequired')"
            />
            <div class="text-muted-foreground mt-1 text-xs">
              <span>{{
                $t('resource.task.instance.fpsTiersHint', {
                  tiers: fpsTiersDisplay,
                })
              }}</span>
              <span v-if="maxFps" class="ml-2">
                ({{ $t('resource.task.instance.maxFpsHint', { max: maxFps }) }})
              </span>
            </div>
          </FormItem>

          <!-- 运动检测门控 (Motion Gate) -->
          <FormItem
            :label="$t('resource.task.instance.motionGate')"
            class="mb-3"
          >
            <div
              class="flex flex-col gap-2 rounded-xl border border-border/80 bg-muted/20 p-3"
            >
              <div class="flex items-center justify-between">
                <div class="flex flex-col">
                  <span class="text-xs font-medium text-foreground">
                    {{
                      motionGateEnabled
                        ? $t('system.common.enable')
                        : $t('system.common.disable')
                    }}
                  </span>
                  <span class="text-xs text-muted-foreground">
                    {{ $t('resource.task.instance.motionGateHint') }}
                  </span>
                </div>
                <Switch v-model:checked="motionGateEnabled" />
              </div>

              <div
                v-if="motionGateEnabled"
                class="mt-2 flex items-center justify-between border-t border-border/60 pt-2"
              >
                <div class="flex flex-col">
                  <span class="text-xs font-medium text-foreground">
                    {{ $t('resource.task.instance.motionGateKeepalive') }}
                  </span>
                  <span class="text-xs text-muted-foreground">
                    {{ $t('resource.task.instance.motionGateKeepaliveHint') }}
                  </span>
                </div>
                <InputNumber
                  v-model:value="motionGateKeepaliveMs"
                  :min="500"
                  :max="30000"
                  :step="500"
                  :precision="0"
                  class="w-32"
                />
              </div>
            </div>
          </FormItem>

          <!-- 算法参数配置 (SchemaForm 驱动) -->
          <FormItem :label="$t('resource.task.instance.params')" class="mb-3">
            <div
              class="rounded-xl border border-border/80 bg-card p-3.5 shadow-2xs"
            >
              <SchemaForm
                ref="schemaFormRef"
                v-model:value="paramsJson"
                :schema="currentConfigSchema"
              />
            </div>
          </FormItem>

          <!-- 新建时是否立即启用 -->
          <FormItem
            v-if="!isEdit"
            :label="$t('resource.task.instance.autoEnable')"
            class="mb-1"
          >
            <div class="flex items-center gap-2">
              <Switch v-model:checked="autoEnable" />
              <span class="text-xs text-muted-foreground">
                {{
                  autoEnable
                    ? $t('resource.task.instance.autoEnableOn')
                    : $t('resource.task.instance.autoEnableOff')
                }}
              </span>
            </div>
          </FormItem>
        </Form>
      </TabPane>

      <!-- Tab 2: 区域与警戒线绘制 -->
      <TabPane key="rules">
        <template #tab>
          <span class="flex items-center gap-1.5 px-1 font-medium">
            <span>📐</span>
            <span>{{ $t('resource.task.instance.tabRules') }}</span>
            <Tag
              v-if="rules.length > 0"
              color="blue"
              class="m-0 text-[10px] px-1 py-0"
            >
              {{ rules.length }}
            </Tag>
          </span>
        </template>

        <div class="pt-2 space-y-2.5">
          <!-- 生效目标提示栏 -->
          <div
            class="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-border/80 bg-muted/20 px-3 py-2 text-xs"
          >
            <div class="flex flex-wrap items-center gap-1.5 overflow-hidden">
              <span class="text-muted-foreground font-medium shrink-0">
                {{ $t('resource.task.instance.currentTargets') }}:
              </span>
              <div
                v-if="activeTargetClasses.length > 0"
                class="flex flex-wrap items-center gap-1"
              >
                <span
                  v-for="cls in activeTargetClasses.slice(0, 8)"
                  :key="cls"
                  class="inline-flex items-center rounded-md bg-background px-2 py-0.5 text-[11px] font-medium text-foreground border border-border/80 shadow-2xs"
                >
                  {{ formatTargetClass(cls) }}
                </span>
                <span
                  v-if="activeTargetClasses.length > 8"
                  class="text-[11px] text-muted-foreground font-mono"
                >
                  +{{ activeTargetClasses.length - 8 }}...
                </span>
              </div>
              <span v-else class="text-muted-foreground font-medium">
                {{ $t('resource.task.instance.allTargetsActive') }}
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

          <!-- 检测规则绘制全景面板 -->
          <div
            class="rounded-xl border border-border/80 bg-card p-3 shadow-2xs"
          >
            <DetectionRuleEditor
              ref="ruleEditorRef"
              v-model:value="rules"
              :camera-id="props.cameraId"
              :open="visible && activeTab === 'rules'"
            />
          </div>
        </div>
      </TabPane>
    </Tabs>

    <!-- 底部导航与提交操作栏 -->
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

<style scoped>
:deep(.instance-form-tabs .ant-tabs-nav) {
  margin-bottom: 8px;
}
</style>
