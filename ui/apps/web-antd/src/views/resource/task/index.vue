<script lang="ts" setup>
defineOptions({
  name: 'ResourceTask',
});

import type { VxeTableGridOptions } from '#/adapter/vxe-table';

import { onUnmounted, ref } from 'vue';

import { Page } from '@vben/common-ui';
import { $t } from '@vben/locales';

import {
  Button,
  message,
  Popconfirm,
  Space,
  Switch,
  Tag,
  Tooltip,
} from 'ant-design-vue';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  deleteTaskApi,
  getTaskListApi,
  setTaskEnabledApi,
  type TaskApi,
} from '#/api';

import InstanceDrawer from './components/InstanceDrawer.vue';
import TaskFormModal from './components/TaskFormModal.vue';

// 任务新建/编辑弹窗状态
const formModalOpen = ref(false);
const currentEditTask = ref<null | TaskApi.TaskItem>(null);

// 实例抽屉状态
const instanceDrawerOpen = ref(false);
const activeDrawerCameraId = ref('');
const activeDrawerTaskName = ref('');

// 轮询定时器
let pollTimer: ReturnType<typeof setInterval> | null = null;
let pollStartTime = 0;
const MAX_POLL_DURATION_MS = 15000;

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
    await gridApi.reload();
  }, 1000);
}

onUnmounted(() => {
  stopPolling();
});

function getStatusTag(status: TaskApi.TaskStatusCode) {
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
      return { color: 'warning', text: '重连中' };
    }
    case 5: {
      return { color: 'default', text: '已停止' };
    }
    case 6: {
      return { color: 'error', text: '异常' };
    }
    default: {
      return { color: 'default', text: '未指定' };
    }
  }
}

const gridOptions: VxeTableGridOptions<TaskApi.TaskItem> = {
  columns: [
    {
      type: 'seq',
      title: $t('system.common.index'),
      width: 60,
      align: 'center',
    },
    { field: 'name', title: $t('resource.task.name'), minWidth: 150 },
    {
      field: 'cameraId',
      title: $t('resource.task.camera'),
      minWidth: 200,
      slots: { default: 'camera' },
    },
    {
      field: 'desiredEnabled',
      title: $t('resource.task.desiredEnabled'),
      width: 100,
      slots: { default: 'desiredEnabled' },
      align: 'center',
    },
    {
      field: 'actualStatus',
      title: $t('resource.task.actualStatus'),
      width: 120,
      slots: { default: 'actualStatus' },
      align: 'center',
    },
    {
      field: 'lastFrameAt',
      title: $t('resource.task.lastFrameAt'),
      width: 180,
      slots: { default: 'lastFrameAt' },
    },
    {
      field: 'instances',
      title: $t('resource.task.instances'),
      width: 130,
      slots: { default: 'instances' },
      align: 'center',
    },
    {
      field: 'actions',
      fixed: 'right',
      showOverflow: false,
      slots: { default: 'actions' },
      title: $t('system.common.action'),
      width: 160,
    },
  ],
  pagerConfig: {
    enabled: true,
  },
  proxyConfig: {
    ajax: {
      query: async ({ page }, formValues) => {
        let configured: boolean | undefined;
        if (formValues.configured === 'yes') configured = true;
        else if (formValues.configured === 'no') configured = false;

        const res = await getTaskListApi({
          page: page.currentPage,
          pageSize: page.pageSize,
          name: formValues.name || undefined,
          configured,
        });

        // 检查是否有任务处于 STARTING 状态
        const hasStarting = res.items?.some(
          (t) => t.actualStatus === 1,
        );
        if (hasStarting) {
          startPolling();
        } else {
          stopPolling();
        }

        return res;
      },
    },
  },
};

const [Grid, gridApi] = useVbenVxeGrid({
  formOptions: {
    schema: [
      {
        component: 'Input',
        fieldName: 'name',
        label: $t('resource.task.filter.name'),
        componentProps: {
          placeholder: $t('resource.task.filter.namePlaceholder'),
        },
      },
      {
        component: 'Select',
        fieldName: 'configured',
        label: $t('resource.task.filter.configured'),
        componentProps: {
          options: [
            { label: $t('resource.task.filter.all'), value: '' },
            { label: $t('resource.task.filter.configuredYes'), value: 'yes' },
            { label: $t('resource.task.filter.configuredNo'), value: 'no' },
          ],
        },
      },
    ],
  },
  gridOptions,
});

function handleAdd() {
  currentEditTask.value = null;
  formModalOpen.value = true;
}

function handleEdit(row: TaskApi.TaskItem) {
  currentEditTask.value = row;
  formModalOpen.value = true;
}

async function handleToggleEnabled(row: TaskApi.TaskItem, checked: boolean) {
  try {
    await setTaskEnabledApi(row.cameraId, checked);
    message.success($t('system.common.success'));
    gridApi.reload();
    startPolling();
  } catch {
    // 拦截器已统一报错
  }
}

async function handleDelete(row: TaskApi.TaskItem) {
  try {
    await deleteTaskApi(row.cameraId);
    message.success($t('system.common.success'));
    gridApi.reload();
  } catch {
    // 拦截器统一报错
  }
}

function handleOpenDrawer(row: TaskApi.TaskItem) {
  activeDrawerCameraId.value = row.cameraId;
  activeDrawerTaskName.value = row.name;
  instanceDrawerOpen.value = true;
}

function handleFormSuccess() {
  gridApi.reload();
  startPolling();
}
</script>

<template>
  <Page :auto-content-height="true">
    <Grid>
      <template #toolbar-tools>
        <Button type="primary" @click="handleAdd">
          {{ $t('resource.task.add') }}
        </Button>
      </template>

      <template #camera="{ row }">
        <span class="font-mono text-xs">{{ row.cameraId }}</span>
      </template>

      <template #desiredEnabled="{ row }">
        <Switch
          :checked="row.desiredEnabled"
          size="small"
          @change="(val) => handleToggleEnabled(row, Boolean(val))"
        />
      </template>

      <template #actualStatus="{ row }">
        <Tooltip v-if="row.statusMessage" :title="row.statusMessage">
          <Tag :color="getStatusTag(row.actualStatus).color">
            {{ getStatusTag(row.actualStatus).text }} ⓘ
          </Tag>
        </Tooltip>
        <Tag v-else :color="getStatusTag(row.actualStatus).color">
          {{ getStatusTag(row.actualStatus).text }}
        </Tag>
      </template>

      <template #lastFrameAt="{ row }">
        <span v-if="row.lastFrameAt" class="text-xs">
          {{ new Date(row.lastFrameAt).toLocaleString() }}
        </span>
        <span v-else class="text-muted-foreground text-xs">
          {{ $t('resource.task.status.waiting') }}
        </span>
      </template>

      <template #instances="{ row }">
        <Button type="link" size="small" @click="handleOpenDrawer(row)">
          {{ $t('resource.task.instances') }}
        </Button>
      </template>

      <template #actions="{ row }">
        <Space :size="8">
          <Button type="link" size="small" @click="handleEdit(row)">
            {{ $t('resource.task.edit') }}
          </Button>
          <Popconfirm
            :title="$t('resource.task.deleteConfirm')"
            @confirm="handleDelete(row)"
          >
            <Button type="link" danger size="small">
              {{ $t('resource.task.delete') }}
            </Button>
          </Popconfirm>
        </Space>
      </template>
    </Grid>

    <!-- 任务创建/编辑 Modal -->
    <TaskFormModal
      v-model:open="formModalOpen"
      :task="currentEditTask"
      @success="handleFormSuccess"
    />

    <!-- 实例抽屉 -->
    <InstanceDrawer
      v-model:open="instanceDrawerOpen"
      :camera-id="activeDrawerCameraId"
      :task-name="activeDrawerTaskName"
      @change="gridApi.reload()"
    />
  </Page>
</template>
