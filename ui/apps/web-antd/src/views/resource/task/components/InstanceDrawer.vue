<script lang="ts" setup>
import type { TaskApi } from '#/api';

import { computed, onUnmounted, ref, watch } from 'vue';

import { $t } from '@vben/locales';

import {
  Button,
  Drawer,
  message,
  Popconfirm,
  Space,
  Switch,
  Table,
  Tag,
  Tooltip,
} from 'ant-design-vue';

import {
  deleteInstanceApi,
  getInstanceListApi,
  setInstanceEnabledApi,
} from '#/api';
import { formatAlgorithmName } from '#/utils/i18n';

import InstanceFormModal from './InstanceFormModal.vue';

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
const instances = ref<TaskApi.InstanceItem[]>([]);

// 实例新增/编辑弹窗
const formModalOpen = ref(false);
const currentEditInstance = ref<null | TaskApi.InstanceItem>(null);

// 轮询控制
let pollTimer: null | ReturnType<typeof setInterval> = null;
let isFetchingInstances = false;
const POLL_INTERVAL_MS = 2000;

function hasActiveInstances(items: TaskApi.InstanceItem[]): boolean {
  return items.some(
    (inst) => inst.actualStatus === 1 || inst.actualStatus === 2,
  );
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

async function loadInstances(silent = false) {
  if (!props.cameraId) return;
  if (silent && isFetchingInstances) return;
  if (!silent) loading.value = true;
  if (silent) isFetchingInstances = true;
  try {
    const list = await getInstanceListApi(props.cameraId);
    instances.value = list || [];

    // 检查是否有处于活跃运行态或启动中的实例，有则保持轮询
    if (hasActiveInstances(instances.value)) {
      startPolling();
    } else {
      stopPolling();
    }
  } catch {
    if (!silent) {
      message.error($t('resource.task.instance.loadFailed'));
    }
  } finally {
    if (!silent) loading.value = false;
    if (silent) isFetchingInstances = false;
  }
}

function startPolling() {
  if (pollTimer) return;
  pollTimer = setInterval(async () => {
    await loadInstances(true);
  }, POLL_INTERVAL_MS);
}

function handleVisibilityChange() {
  if (document.hidden) {
    stopPolling();
  } else if (props.open && props.cameraId) {
    loadInstances(true);
  }
}

// 抽屉开关监听
watch(
  () => props.open,
  (isOpen) => {
    if (isOpen && props.cameraId) {
      loadInstances();
      document.addEventListener('visibilitychange', handleVisibilityChange);
    } else {
      document.removeEventListener('visibilitychange', handleVisibilityChange);
      stopPolling();
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

async function handleToggleEnabled(
  inst: TaskApi.InstanceItem,
  checked: boolean,
) {
  try {
    await setInstanceEnabledApi(inst.instanceId, checked);
    message.success($t('system.common.success'));
    emit('change');
    await loadInstances();
    startPolling();
  } catch {
    // 拦截器已统一报错
  }
}

async function handleDelete(inst: TaskApi.InstanceItem) {
  try {
    await deleteInstanceApi(inst.instanceId);
    message.success($t('system.common.success'));
    emit('change');
    await loadInstances();
  } catch {
    // 拦截器统一报错
  }
}

function handleFormSuccess() {
  emit('change');
  loadInstances();
  startPolling();
}

const columns = [
  {
    title: $t('resource.task.instance.algorithm'),
    dataIndex: 'algorithmId',
    key: 'algorithmId',
    width: 140,
  },
  {
    title: $t('resource.task.instance.analysisFps'),
    dataIndex: 'analysisFps',
    key: 'analysisFps',
    width: 90,
  },
  {
    title: $t('resource.task.instance.enabled'),
    dataIndex: 'enabled',
    key: 'enabled',
    width: 80,
  },
  {
    title: $t('resource.task.instance.actualStatus'),
    dataIndex: 'actualStatus',
    key: 'actualStatus',
    width: 110,
  },
  {
    title: $t('resource.task.instance.currentFps'),
    dataIndex: 'currentFps',
    key: 'currentFps',
    width: 90,
  },
  {
    title: $t('resource.task.instance.reportedAt'),
    dataIndex: 'reportedAt',
    key: 'reportedAt',
    width: 160,
  },
  {
    title: $t('system.common.action'),
    key: 'action',
    width: 120,
    fixed: 'right' as const,
  },
];

function getStatusTag(status: number) {
  switch (status) {
    case 1: {
      return { color: 'processing', textKey: 'resource.task.status.starting' };
    }
    case 2: {
      return { color: 'success', textKey: 'resource.task.status.running' };
    }
    case 3: {
      return { color: 'warning', textKey: 'resource.task.status.degraded' };
    }
    case 4: {
      return { color: 'default', textKey: 'resource.task.status.stopped' };
    }
    case 5: {
      return { color: 'error', textKey: 'resource.task.status.error' };
    }
    default: {
      return {
        color: 'default',
        textKey: 'resource.task.status.unspecified',
      };
    }
  }
}
</script>

<template>
  <Drawer
    v-model:open="visible"
    :title="`${$t('resource.task.instance.drawerTitle')} - ${taskName || cameraId}`"
    width="880px"
    destroy-on-close
  >
    <div class="flex h-full flex-col">
      <!-- 头部操作区 -->
      <div class="mb-4 flex items-center justify-between">
        <div class="text-muted-foreground text-sm">
          <span>{{ $t('resource.task.instance.camera') }}: </span>
          <span class="font-mono font-medium">{{ cameraId }}</span>
        </div>
        <Button
          v-access:code="['resource:task:add']"
          type="primary"
          @click="handleAdd"
        >
          {{ $t('resource.task.instance.add') }}
        </Button>
      </div>

      <!-- 表格区 -->
      <div class="flex-1 overflow-auto">
        <Table
          :data-source="instances"
          :columns="columns"
          :loading="loading"
          :pagination="false"
          row-key="instanceId"
          size="middle"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'algorithmId'">
              <span class="font-medium">
                {{ formatAlgorithmName(record.algorithmId) }}
              </span>
              <span class="text-xs text-muted-foreground ml-1">
                ({{ record.algorithmId }})
              </span>
            </template>

            <template v-else-if="column.key === 'analysisFps'">
              <span>{{ record.analysisFps }} FPS</span>
            </template>

            <template v-else-if="column.key === 'enabled'">
              <Switch
                v-access:code="['resource:task:edit']"
                :checked="record.enabled"
                size="small"
                @change="
                  (val) =>
                    handleToggleEnabled(
                      record as TaskApi.InstanceItem,
                      Boolean(val),
                    )
                "
              />
            </template>

            <template v-else-if="column.key === 'actualStatus'">
              <Tooltip
                v-if="record.statusMessage"
                :title="record.statusMessage"
              >
                <Tag :color="getStatusTag(record.actualStatus).color">
                  {{ $t(getStatusTag(record.actualStatus).textKey) }} ⓘ
                </Tag>
              </Tooltip>
              <Tag v-else :color="getStatusTag(record.actualStatus).color">
                {{ $t(getStatusTag(record.actualStatus).textKey) }}
              </Tag>
            </template>

            <template v-else-if="column.key === 'currentFps'">
              <span
                v-if="
                  record.currentFps !== null && record.currentFps !== undefined
                "
              >
                {{ Number(record.currentFps).toFixed(1) }}
              </span>
              <span v-else class="text-muted-foreground">-</span>
            </template>

            <template v-else-if="column.key === 'reportedAt'">
              <span v-if="record.reportedAt" class="text-xs">
                {{ new Date(record.reportedAt).toLocaleString() }}
              </span>
              <span v-else class="text-muted-foreground text-xs">
                {{ $t('resource.task.status.waiting') }}
              </span>
            </template>

            <template v-else-if="column.key === 'action'">
              <Space :size="8">
                <Button
                  v-access:code="['resource:task:edit']"
                  type="link"
                  size="small"
                  @click="handleEdit(record as TaskApi.InstanceItem)"
                >
                  {{ $t('resource.task.instance.edit') }}
                </Button>
                <Popconfirm
                  :title="$t('resource.task.instance.deleteConfirm')"
                  @confirm="handleDelete(record as TaskApi.InstanceItem)"
                >
                  <Button
                    v-access:code="['resource:task:delete']"
                    type="link"
                    danger
                    size="small"
                  >
                    {{ $t('resource.task.instance.delete') }}
                  </Button>
                </Popconfirm>
              </Space>
            </template>
          </template>
        </Table>
      </div>
    </div>

    <!-- 实例创建/编辑 Modal -->
    <InstanceFormModal
      v-model:open="formModalOpen"
      :camera-id="cameraId"
      :instance="currentEditInstance"
      @success="handleFormSuccess"
    />
  </Drawer>
</template>
