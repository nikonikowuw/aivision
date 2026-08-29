<script lang="ts" setup>
import type { AlgorithmApi } from '#/api';

import { computed, nextTick, ref, watch } from 'vue';

import { $t } from '@vben/locales';

import {
  Form,
  FormItem,
  Input,
  InputNumber,
  message,
  Modal,
  Select,
  Switch,
  Tag,
} from 'ant-design-vue';

import {
  createInstanceApi,
  getAlgorithmList,
  type TaskApi,
  updateInstanceApi,
} from '#/api';

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

const loading = ref(false);
const submitting = ref(false);
const schemaFormRef = ref<InstanceType<typeof SchemaForm>>();

// 算法列表
const algorithmList = ref<AlgorithmApi.AlgorithmItem[]>([]);
const selectedAlgorithmId = ref<string>('');
const analysisFps = ref<number>(25);
const autoEnable = ref<boolean>(true);
const paramsJson = ref<Record<string, unknown>>({});
const rulesRaw = ref<string>('[]');

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
  const sorted = [...fpsTiers.value].sort((a, b) => b.fps - a.fps);
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
      label: `${a.name} (${a.algorithmId})`,
      value: a.algorithmId,
    }));
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

    await loadAlgorithms();

    if (props.instance) {
      // 编辑回显
      selectedAlgorithmId.value = props.instance.algorithmId;
      analysisFps.value = props.instance.analysisFps || 25;
      rulesRaw.value = JSON.stringify(props.instance.rules || [], null, 2);

      if (typeof props.instance.paramsJson === 'string') {
        try {
          paramsJson.value = JSON.parse(props.instance.paramsJson || '{}');
        } catch {
          paramsJson.value = {};
        }
      } else {
        paramsJson.value = props.instance.paramsJson || {};
      }
    } else {
      // 新建默认
      analysisFps.value = 25;
      autoEnable.value = true;
      paramsJson.value = {};
      rulesRaw.value = '[]';
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
    message.error($t('resource.task.instance.paramsInvalid'));
    return;
  }

  // 2. 解析规则 JSON
  let parsedRules: TaskApi.DetectionRule[];
  try {
    const parsed = JSON.parse(rulesRaw.value || '[]');
    if (!Array.isArray(parsed)) {
      message.error($t('resource.task.instance.rulesJsonInvalid'));
      return;
    }
    parsedRules = parsed;
  } catch {
    message.error($t('resource.task.instance.rulesJsonInvalid'));
    return;
  }

  submitting.value = true;
  try {
    if (isEdit.value && props.instance) {
      await updateInstanceApi(props.instance.instanceId, {
        analysisFps: analysisFps.value,
        paramsJson: paramsJson.value,
        rules: parsedRules,
      });
      message.success($t('system.common.success'));
    } else {
      await createInstanceApi({
        cameraId: props.cameraId,
        algorithmId: selectedAlgorithmId.value,
        analysisFps: analysisFps.value,
        paramsJson: paramsJson.value,
        rules: parsedRules,
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
    width="680px"
    @ok="handleOk"
  >
    <Form layout="vertical" class="mt-4">
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
          :placeholder="$t('resource.task.instance.algorithmSelectPlaceholder')"
          class="w-full"
        />
        <div
          v-if="currentAlgorithm"
          class="text-muted-foreground mt-1.5 flex items-center gap-2 text-xs"
        >
          <span>{{ $t('resource.task.instance.activeVersion') }}:</span>
          <Tag v-if="currentAlgorithm.activeVersion" color="blue">
            {{ currentAlgorithm.activeVersion }}
          </Tag>
          <Tag v-else color="red">
            {{ $t('resource.task.instance.noActiveVersion') }}
          </Tag>
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

      <!-- 新建时是否立即启用 -->
      <FormItem
        v-if="!isEdit"
        :label="$t('resource.task.instance.autoEnable')"
        class="mb-3"
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

      <!-- 算法参数配置 (SchemaForm 驱动) -->
      <FormItem :label="$t('resource.task.instance.params')" class="mb-3">
        <div class="rounded-md border p-3">
          <SchemaForm
            ref="schemaFormRef"
            v-model:value="paramsJson"
            :schema="currentConfigSchema"
          />
        </div>
      </FormItem>

      <!-- 检测规则 -->
      <FormItem :label="$t('resource.task.instance.rules')" class="mb-2">
        <Input.TextArea
          v-model:value="rulesRaw"
          :rows="3"
          :placeholder="$t('resource.task.instance.rulesPlaceholder')"
        />
        <div class="text-muted-foreground mt-1 text-xs">
          <span>{{ $t('resource.task.instance.rulesHint') }}</span>
        </div>
      </FormItem>
    </Form>
  </Modal>
</template>
