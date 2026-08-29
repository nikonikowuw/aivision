<script lang="ts" setup>
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
  type TaskApi,
} from '#/api';

import InstanceFormModal from './InstanceFormModal.vue';

interface Props {
  open: boolean;
  cameraId: string;
  taskName: string;
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
let pollTimer: ReturnType<typeof setInterval> | null = null;
let pollStartTime = 0;
const MAX_POLL_DURATION_MS = 15000;

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

async function loadInstances(silent = false) {
  if (!props.cameraId) return;
  if (!silent) loading.value = true;
  try {
    const list = await getInstanceListApi(props.cameraId);
    instances.value = list || [];

    // 检查是否有处于 STARTING 中间态的实例
    const hasStarting = instances.value.some((inst) => inst.actualStatus === 1);
    if (hasStarting) {
      startPolling();
    } else {
      stopPolling();
    }
  } catch (err: any) {
    if (!silent) {
      message.error(err.message || '加载算法实例失败');
    }
  } finally {
    if (!silent) loading.value = false;
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
    await loadInstances(true);
  }, 1000);
}

// 抽屉开关监听
watch(
  () => props.open,
  (isOpen) => {
    if (isOpen && props.cameraId) {
      loadInstances();
    } else {
      stopPolling();
    }
  },
);

onUnmounted(() => {
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
    title: '算法',
    dataIndex: 'algorithmId',
    key: 'algorithmId',
    width: 140,
  },
  {
    title: '采样帧率',
    dataIndex: 'analysisFps',
    key: 'analysisFps',
    width: 90,
  },
  {
    title: '启用',
    dataIndex: 'enabled',
    key: 'enabled',
    width: 80,
  },
  {
    title: '状态',
    dataIndex: 'actualStatus',
    key: 'actualStatus',
    width: 110,
  },
  {
    title: '实时 FPS',
    dataIndex: 'currentFps',
    key: 'currentFps',
    width: 90,
  },
  {
    title: '上报时间',
    dataIndex: 'reportedAt',
    key: 'reportedAt',
    width: 160,
  },
  {
    title: '操作',
    key: 'action',
    width: 120,
    fixed: 'right' as const,
  },
];

function getStatusTag(status: number) {
  switch (status) {
    case 1: {
      return { color: 'processing', text: '启动中' };
    }
    case 2: {
      return { color: 'success', text: '运行中' };
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
      return { color: 'default', text: '未就绪' };
    }
  }
}
</script>

<template>
  <Drawer
    v-model:open="visible"
    :title="`算法实例管理 - ${taskName || cameraId}`"
    width="880px"
    destroy-on-close
  >
    <div class="flex h-full flex-col">
      <!-- 头部操作区 -->
      <div class="mb-4 flex items-center justify-between">
        <div class="text-muted-foreground text-sm">
          <span>摄像头: </span>
          <span class="font-mono font-medium">{{ cameraId }}</span>
        </div>
        <Button type="primary" @click="handleAdd">
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
              <span class="font-medium">{{ record.algorithmId }}</span>
            </template>

            <template v-else-if="column.key === 'analysisFps'">
              <span>{{ record.analysisFps }} FPS</span>
            </template>

            <template v-else-if="column.key === 'enabled'">
              <Switch
                :checked="record.enabled"
                size="small"
                @change="(val) => handleToggleEnabled(record as TaskApi.InstanceItem, Boolean(val))"
              />
            </template>

            <template v-else-if="column.key === 'actualStatus'">
              <Tooltip v-if="record.statusMessage" :title="record.statusMessage">
                <Tag :color="getStatusTag(record.actualStatus).color">
                  {{ getStatusTag(record.actualStatus).text }} ⓘ
                </Tag>
              </Tooltip>
              <Tag v-else :color="getStatusTag(record.actualStatus).color">
                {{ getStatusTag(record.actualStatus).text }}
              </Tag>
            </template>

            <template v-else-if="column.key === 'currentFps'">
              <span v-if="record.currentFps !== null && record.currentFps !== undefined">
                {{ Number(record.currentFps).toFixed(1) }}
              </span>
              <span v-else class="text-muted-foreground">-</span>
            </template>

            <template v-else-if="column.key === 'reportedAt'">
              <span v-if="record.reportedAt" class="text-xs">
                {{ new Date(record.reportedAt).toLocaleString() }}
              </span>
              <span v-else class="text-muted-foreground text-xs">等待上报</span>
            </template>

            <template v-else-if="column.key === 'action'">
              <Space :size="8">
                <Button
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
                  <Button type="link" danger size="small">
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
