<script lang="ts" setup>
defineOptions({
  name: 'ResourceTask',
});

import type { VxeTableGridOptions } from '#/adapter/vxe-table';

import {
  computed,
  onActivated,
  onDeactivated,
  onMounted,
  onUnmounted,
  ref,
} from 'vue';

import { Page } from '@vben/common-ui';
import { $t } from '@vben/locales';

import {
  Button,
  message,
  Popconfirm,
  Progress,
  Space,
  Switch,
  Tag,
  Tooltip,
} from 'ant-design-vue';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  batchDeleteTasksApi,
  deleteInstanceApi,
  deleteTaskApi,
  getInstanceListApi,
  getTaskListApi,
  getTaskStatsApi,
  setInstanceEnabledApi,
  setTaskEnabledApi,
  type TaskApi,
} from '#/api';

import InstanceDrawer from './components/InstanceDrawer.vue';
import InstanceFormModal from './components/InstanceFormModal.vue';
import TaskFormModal from './components/TaskFormModal.vue';

// 选中的任务项（供批量删除）
const selectedTasks = ref<TaskApi.TaskItem[]>([]);

// 任务新建/编辑弹窗状态
const formModalOpen = ref(false);
const currentEditTask = ref<null | TaskApi.TaskItem>(null);

// 实例抽屉状态
const instanceDrawerOpen = ref(false);
const activeDrawerCameraId = ref('');
const activeDrawerTaskName = ref('');

// 实例快速新建/编辑弹窗状态
const instanceFormModalOpen = ref(false);
const activeTargetCameraId = ref('');
const currentEditInstance = ref<null | TaskApi.InstanceItem>(null);

// 任务管理概览统计（在线任务/已调度实例/计算单元负载）
const taskStats = ref<null | TaskApi.TaskStats>(null);

async function loadStats() {
  try {
    taskStats.value = await getTaskStatsApi();
  } catch {
    // 拦截器已统一报错；统计条保持上次值
  }
}

// 计算单元负载百分比：used / total（对齐原型 used/total 口径）
const loadPercent = computed(() => {
  const s = taskStats.value;
  if (!s || s.totalUnits <= 0) return 0;
  return Math.min(100, Math.round((s.usedUnits / s.totalUnits) * 100));
});

// 轮询定时器（静默就地更新，不触发 VxeTable loading 遮罩与全量重排）
let pollTimer: null | ReturnType<typeof setInterval> = null;
let isFetchingStatus = false;
let isViewActive = true;
const POLL_INTERVAL_MS = 2000;

function hasActiveRunningOrStarting(items?: TaskApi.TaskItem[]): boolean {
  if (!items || items.length === 0) return false;
  return items.some(
    (t) =>
      t.actualStatus === 1 ||
      t.actualStatus === 2 ||
      t.instances?.some((i) => i.actualStatus === 1 || i.actualStatus === 2),
  );
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

async function fetchStatusSilently() {
  if (isFetchingStatus) return;
  isFetchingStatus = true;
  try {
    loadStats();
    const formValues = (await gridApi.formApi.getValues?.()) ?? {};
    let configured: boolean | undefined;
    if (formValues.configured === 'yes') configured = true;
    else if (formValues.configured === 'no') configured = false;

    // 获取当前分页信息
    const tablePage = gridApi.grid?.getProxyInfo?.()?.pager || {
      currentPage: 1,
      pageSize: 10,
    };

    const res = await getTaskListApi({
      page: tablePage.currentPage,
      pageSize: tablePage.pageSize,
      name: formValues.name || undefined,
      configured,
    });

    // 若当前没有任何任务处于活跃推理或启动状态，自动休眠轮询
    if (!hasActiveRunningOrStarting(res.items)) {
      stopPolling();
    }

    // 获取当前表格响应式数据行（使用 getTableData().tableData 或 getFullData，确保拿到的是响应式 Proxy）
    const tableDataRes = gridApi.grid?.getTableData?.();
    const currentRows: TaskApi.TaskItem[] = tableDataRes?.tableData?.length
      ? tableDataRes.tableData
      : (gridApi.grid?.getFullData?.() ?? gridApi.grid?.getData?.() ?? []);
    if (currentRows.length > 0 && res.items?.length > 0) {
      const freshMap = new Map(res.items.map((item) => [item.cameraId, item]));
      for (const row of currentRows) {
        const fresh = freshMap.get(row.cameraId);
        if (fresh) {
          row.actualStatus = fresh.actualStatus;
          row.desiredEnabled = fresh.desiredEnabled;
          row.statusMessage = fresh.statusMessage;
          row.lastFrameAt = fresh.lastFrameAt;
          row.reportedAt = fresh.reportedAt;
          row.instanceCount = fresh.instanceCount;

          if (row.instances && fresh.instances) {
            const freshInstMap = new Map(
              fresh.instances.map((i) => [i.instanceId, i]),
            );
            for (const inst of row.instances) {
              const freshInst = freshInstMap.get(inst.instanceId);
              if (freshInst) {
                inst.actualStatus = freshInst.actualStatus;
                inst.currentFps = freshInst.currentFps;
                inst.analysisFps = freshInst.analysisFps;
                inst.enabled = freshInst.enabled;
                inst.rulesCount = freshInst.rulesCount;
                inst.statusMessage = freshInst.statusMessage;
              }
            }
            if (row.instances.length !== fresh.instances.length) {
              row.instances = fresh.instances;
            }
          } else {
            row.instances = fresh.instances;
          }
        }
      }
    }
  } catch {
    // 静默轮询失败不打扰用户
  } finally {
    isFetchingStatus = false;
  }
}

function startPolling() {
  if (pollTimer) return;
  pollTimer = setInterval(async () => {
    await fetchStatusSilently();
  }, POLL_INTERVAL_MS);
}

async function refreshAfterResume() {
  await fetchStatusSilently();
  const currentRows: TaskApi.TaskItem[] = gridApi.grid?.getData?.() ?? [];
  if (hasActiveRunningOrStarting(currentRows)) {
    startPolling();
  } else {
    stopPolling();
  }
}

async function handleVisibilityChange() {
  if (document.hidden) {
    stopPolling();
  } else if (isViewActive) {
    await refreshAfterResume();
  }
}

onActivated(() => {
  isViewActive = true;
  void refreshAfterResume();
});

onDeactivated(() => {
  isViewActive = false;
  stopPolling();
});

onMounted(() => {
  document.addEventListener('visibilitychange', handleVisibilityChange);
});

onUnmounted(() => {
  document.removeEventListener('visibilitychange', handleVisibilityChange);
  stopPolling();
});

function getTaskStatusTag(status: TaskApi.TaskStatusCode) {
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
      return { color: 'warning', textKey: 'resource.task.status.reconnecting' };
    }
    case 5: {
      return { color: 'default', textKey: 'resource.task.status.stopped' };
    }
    case 6: {
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

function getInstanceStatusTag(status: number) {
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

function getInstanceDotClass(status: number, enabled: boolean): string {
  if (!enabled) return 'bg-muted-foreground/60';
  switch (status) {
    case 1: {
      return 'bg-blue-500 animate-pulse';
    }
    case 2: {
      return 'bg-emerald-500';
    }
    case 3: {
      return 'bg-amber-500';
    }
    case 5: {
      return 'bg-rose-500';
    }
    default: {
      return 'bg-muted-foreground/60';
    }
  }
}

const gridOptions: VxeTableGridOptions<TaskApi.TaskItem> = {
  checkboxConfig: {
    highlight: true,
  },
  rowConfig: {
    keyField: 'cameraId',
  },
  expandConfig: {
    trigger: 'cell',
    showIcon: true,
    reserve: true,
  },
  columns: [
    { type: 'checkbox', width: 46, align: 'center' },
    {
      type: 'expand',
      width: 46,
      align: 'center',
      slots: { content: 'expandContent' },
    },
    {
      type: 'seq',
      title: $t('system.common.index'),
      width: 50,
      align: 'center',
    },
    {
      field: 'name',
      title: $t('resource.task.name'),
      width: 180,
      slots: { default: 'taskName' },
    },
    {
      field: 'actualStatus',
      title: $t('resource.task.actualStatus'),
      width: 100,
      slots: { default: 'actualStatus' },
      align: 'center',
    },
    {
      field: 'desiredEnabled',
      title: $t('resource.task.desiredEnabled'),
      width: 90,
      slots: { default: 'desiredEnabled' },
      align: 'center',
    },
    {
      field: 'instances',
      title: $t('resource.task.instance.pipeline'),
      minWidth: 320,
      slots: { default: 'instances' },
    },
    {
      field: 'lastFrameAt',
      title: $t('resource.task.lastFrameAt'),
      width: 160,
      slots: { default: 'lastFrameAt' },
    },
    {
      field: 'actions',
      fixed: 'right',
      showOverflow: false,
      slots: { default: 'actions' },
      title: $t('system.common.action'),
      width: 200,
      align: 'center',
    },
  ],
  pagerConfig: {
    enabled: true,
  },
  proxyConfig: {
    ajax: {
      query: async ({ page }, formValues) => {
        // 统计条与列表同频刷新（首次加载 + 每次 reload + 轮询）
        loadStats();

        let configured: boolean | undefined;
        if (formValues.configured === 'yes') configured = true;
        else if (formValues.configured === 'no') configured = false;

        const res = await getTaskListApi({
          page: page.currentPage,
          pageSize: page.pageSize,
          name: formValues.name || undefined,
          configured,
        });

        // 检查是否有任务或实例处于活跃状态（STARTING 或 RUNNING），有则保持静默轮询
        if (hasActiveRunningOrStarting(res.items)) {
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
  gridEvents: {
    checkboxAll: ({ records }: { records: TaskApi.TaskItem[] }) => {
      selectedTasks.value = records;
    },
    checkboxChange: ({ records }: { records: TaskApi.TaskItem[] }) => {
      selectedTasks.value = records;
    },
  },
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
    await gridApi.query();
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

function handleClearSelection() {
  gridApi.grid?.clearCheckboxRow();
  selectedTasks.value = [];
}

async function handleBatchDelete() {
  if (selectedTasks.value.length === 0) return;
  try {
    const cameraIds = selectedTasks.value.map((t) => t.cameraId);
    await batchDeleteTasksApi({ cameraIds });
    message.success($t('system.common.success'));
    handleClearSelection();
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

function handleOpenAddInstance(row: TaskApi.TaskItem) {
  activeTargetCameraId.value = row.cameraId;
  currentEditInstance.value = null;
  instanceFormModalOpen.value = true;
}

async function handleOpenEditInstance(
  row: TaskApi.TaskItem,
  brief: TaskApi.TaskInstanceBrief,
) {
  activeTargetCameraId.value = row.cameraId;
  try {
    const list = await getInstanceListApi(row.cameraId);
    const target = list.find((i) => i.instanceId === brief.instanceId);
    if (target) {
      currentEditInstance.value = target;
      instanceFormModalOpen.value = true;
    } else {
      message.error($t('resource.task.instance.detailNotFound'));
    }
  } catch {
    message.error($t('resource.task.instance.loadDetailFailed'));
  }
}

async function handleToggleInstanceEnabled(
  brief: TaskApi.TaskInstanceBrief,
  checked: boolean,
) {
  try {
    await setInstanceEnabledApi(brief.instanceId, checked);
    message.success($t('system.common.success'));
    await gridApi.query();
    startPolling();
  } catch {
    // 拦截器已报错
  }
}

async function handleDeleteInstance(brief: TaskApi.TaskInstanceBrief) {
  try {
    await deleteInstanceApi(brief.instanceId);
    message.success($t('system.common.success'));
    gridApi.reload();
  } catch {
    // 拦截器已报错
  }
}

function handleFormSuccess() {
  gridApi.reload();
  startPolling();
}
</script>

<template>
  <Page :auto-content-height="true">
    <!-- 概览统计条：在线任务 / 已调度实例 / 计算单元负载 -->
    <div
      class="mb-3 flex flex-wrap items-center gap-x-6 gap-y-2 rounded-lg border border-border bg-card px-4 py-3 text-xs"
    >
      <div class="flex items-center gap-2">
        <span class="text-muted-foreground">
          {{ $t('resource.task.stats.onlineTasks') }}:
        </span>
        <span
          class="font-mono font-bold text-emerald-600 dark:text-emerald-400"
        >
          {{ taskStats?.runningTasks ?? '-' }} /
          {{ taskStats?.totalTasks ?? '-' }}
        </span>
      </div>
      <div class="h-3 w-px bg-border"></div>
      <div class="flex items-center gap-2">
        <span class="text-muted-foreground">
          {{ $t('resource.task.stats.scheduledInstances') }}:
        </span>
        <span class="font-mono font-bold text-blue-600 dark:text-blue-400">
          <template v-if="taskStats">
            {{ taskStats.enabledInstances }}
            {{ $t('resource.task.stats.instanceUnit') }}
          </template>
          <template v-else>-</template>
        </span>
      </div>
      <div class="h-3 w-px bg-border"></div>
      <div class="flex items-center gap-2">
        <span class="text-muted-foreground">
          {{ $t('resource.task.stats.computeLoad') }}:
        </span>
        <span class="font-mono font-bold text-amber-600 dark:text-amber-400">
          <template v-if="taskStats && taskStats.totalUnits > 0">
            {{ taskStats.usedUnits }} / {{ taskStats.totalUnits }} Units ({{
              loadPercent
            }}%)
          </template>
          <template v-else>-</template>
        </span>
      </div>
    </div>

    <!-- 批量操作栏：仅当复选框勾选 > 0 时浮动显示 -->
    <div
      v-if="selectedTasks.length > 0"
      v-access:code="['resource:task:delete']"
      class="mb-3 flex items-center justify-between rounded-lg border border-primary/20 bg-primary/5 px-4 py-2 text-xs"
    >
      <div class="flex items-center gap-2">
        <span class="text-foreground font-medium">
          {{
            $t('system.common.selectedCount', { count: selectedTasks.length })
          }}
        </span>
        <Button
          type="link"
          size="small"
          class="p-0 text-xs"
          @click="handleClearSelection"
        >
          {{ $t('system.common.clearSelection') }}
        </Button>
      </div>
      <div class="flex items-center gap-2">
        <Popconfirm
          :title="
            $t('system.common.confirmBatchDelete', {
              count: selectedTasks.length,
            })
          "
          :ok-text="$t('system.common.confirm')"
          :cancel-text="$t('system.common.cancel')"
          @confirm="handleBatchDelete"
        >
          <Button type="primary" danger size="small">
            {{ $t('system.common.batchDelete') }}
          </Button>
        </Popconfirm>
      </div>
    </div>

    <Grid>
      <template #toolbar-tools>
        <Button
          v-access:code="['resource:task:add']"
          type="primary"
          @click="handleAdd"
        >
          {{ $t('resource.task.add') }}
        </Button>
      </template>

      <!-- 任务名称 + 摄像头信息 -->
      <template #taskName="{ row }">
        <div class="py-1">
          <div
            class="font-medium text-foreground text-sm flex items-center gap-1.5"
          >
            <span>{{ row.name }}</span>
            <span
              v-if="row.instanceCount !== undefined && row.instanceCount > 0"
              class="text-[10px] px-1.5 py-0.5 rounded-full bg-primary/10 text-primary font-mono font-normal"
            >
              {{
                $t('resource.task.instanceBadge', { count: row.instanceCount })
              }}
            </span>
          </div>
          <div class="text-xs text-muted-foreground font-mono mt-0.5">
            {{ row.cameraId }}
          </div>
        </div>
      </template>

      <!-- 任务期望启停 Switch -->
      <template #desiredEnabled="{ row }">
        <Switch
          v-access:code="['resource:task:edit']"
          :checked="row.desiredEnabled"
          size="small"
          @change="(val) => handleToggleEnabled(row, Boolean(val))"
        />
      </template>

      <!-- 任务运行状态 Tag -->
      <template #actualStatus="{ row }">
        <Tooltip v-if="row.statusMessage" :title="row.statusMessage">
          <Tag :color="getTaskStatusTag(row.actualStatus).color">
            {{ $t(getTaskStatusTag(row.actualStatus).textKey) }} ⓘ
          </Tag>
        </Tooltip>
        <Tag v-else :color="getTaskStatusTag(row.actualStatus).color">
          {{ $t(getTaskStatusTag(row.actualStatus).textKey) }}
        </Tag>
      </template>

      <!-- 算法实例流水线（紧凑管道胶囊槽位） -->
      <template #instances="{ row }">
        <div
          v-if="!row.instances || row.instances.length === 0"
          class="flex items-center gap-2 py-1 text-xs text-muted-foreground"
        >
          <span class="italic">{{
            $t('resource.task.instance.notConfigured')
          }}</span>
          <Button
            v-access:code="['resource:task:add']"
            type="link"
            size="small"
            class="p-0 text-xs"
            @click="handleOpenAddInstance(row)"
          >
            + {{ $t('resource.task.instance.mountNow') }}
          </Button>
        </div>

        <div v-else class="flex flex-wrap items-center gap-2 py-1">
          <div
            v-for="inst in row.instances"
            :key="inst.instanceId"
            class="flex items-center gap-2 px-2.5 py-1 rounded-md border border-border bg-card shadow-2xs hover:border-primary/60 transition-all"
            :class="inst.enabled ? 'opacity-100' : 'opacity-60'"
          >
            <!-- 点击主体区域打开参数配置 -->
            <div
              v-access:code="['resource:task:edit']"
              class="flex items-center gap-2 cursor-pointer"
              @click="handleOpenEditInstance(row, inst)"
            >
              <!-- 健康指示圆点 -->
              <span
                class="inline-block w-2 h-2 rounded-full shrink-0"
                :class="getInstanceDotClass(inst.actualStatus, inst.enabled)"
              ></span>

              <!-- 算法名与帧率 -->
              <div class="flex flex-col leading-tight">
                <span class="font-medium text-xs text-card-foreground">
                  {{ inst.algorithmId }}
                </span>
                <span
                  class="text-[10px] text-muted-foreground font-mono mt-0.5"
                >
                  <span
                    v-if="
                      inst.currentFps !== null && inst.currentFps !== undefined
                    "
                    :class="
                      inst.currentFps >= inst.analysisFps
                        ? 'text-emerald-600 dark:text-emerald-400 font-semibold'
                        : 'text-amber-600 dark:text-amber-400'
                    "
                  >
                    {{ Number(inst.currentFps).toFixed(1) }}
                  </span>
                  <span v-else>-</span>
                  <span class="opacity-60"> / </span>
                  <span>{{ inst.analysisFps }} FPS</span>
                </span>
              </div>
            </div>

            <!-- 轻量启停开关 -->
            <Switch
              v-access:code="['resource:task:edit']"
              :checked="inst.enabled"
              size="small"
              class="scale-85 origin-right"
              @change="(val) => handleToggleInstanceEnabled(inst, Boolean(val))"
            />
          </div>

          <!-- 快捷添加实例按钮 -->
          <Button
            v-access:code="['resource:task:add']"
            type="dashed"
            size="small"
            class="h-7 px-2 text-xs flex items-center gap-1 text-muted-foreground hover:text-primary"
            @click="handleOpenAddInstance(row)"
          >
            + {{ $t('resource.task.instance.mount') }}
          </Button>
        </div>
      </template>

      <!-- 最后上报时间 -->
      <template #lastFrameAt="{ row }">
        <span v-if="row.lastFrameAt" class="text-xs text-muted-foreground">
          {{ new Date(row.lastFrameAt).toLocaleString() }}
        </span>
        <span v-else class="text-muted-foreground text-xs">
          {{ $t('resource.task.status.waiting') }}
        </span>
      </template>

      <!-- 操作列 -->
      <template #actions="{ row }">
        <Space :size="4">
          <Button type="link" size="small" @click="handleOpenDrawer(row)">
            {{ $t('resource.task.instanceDrawer') }}
          </Button>
          <Button
            v-access:code="['resource:task:edit']"
            type="link"
            size="small"
            @click="handleEdit(row)"
          >
            {{ $t('resource.task.edit') }}
          </Button>
          <Popconfirm
            :title="$t('resource.task.deleteConfirm')"
            @confirm="handleDelete(row)"
          >
            <Button
              v-access:code="['resource:task:delete']"
              type="link"
              danger
              size="small"
            >
              {{ $t('resource.task.delete') }}
            </Button>
          </Popconfirm>
        </Space>
      </template>

      <!-- 展开行：算法实例控制台 (Instance Console) -->
      <template #expandContent="{ row }">
        <div class="px-6 py-4 bg-muted/25 border-y border-border">
          <!-- 展开控制台头部 -->
          <div class="flex items-center justify-between mb-3">
            <div class="flex items-center gap-2">
              <span class="text-xs font-semibold text-foreground tracking-wide">
                {{ $t('resource.task.instance.console') }} ({{
                  row.instances?.length || 0
                }})
              </span>
              <span class="text-xs text-muted-foreground">
                ·
                {{
                  $t('resource.task.instance.sharedDecode', {
                    cameraId: row.cameraId,
                  })
                }}
              </span>
            </div>
            <div class="flex items-center gap-2">
              <Button
                v-access:code="['resource:task:add']"
                size="small"
                type="primary"
                ghost
                @click="handleOpenAddInstance(row)"
              >
                + {{ $t('resource.task.instance.mountNew') }}
              </Button>
              <Button size="small" @click="handleOpenDrawer(row)">
                {{ $t('resource.task.instance.enterDrawer') }}
              </Button>
              <Popconfirm
                :title="$t('resource.task.deleteConfirm')"
                @confirm="handleDelete(row)"
              >
                <Button
                  v-access:code="['resource:task:delete']"
                  size="small"
                  danger
                  ghost
                >
                  {{ $t('resource.task.instance.deleteTask') }}
                </Button>
              </Popconfirm>
            </div>
          </div>

          <!-- 实例为空提示 -->
          <div
            v-if="!row.instances || row.instances.length === 0"
            class="p-6 rounded-lg border border-dashed border-border bg-card text-center text-xs text-muted-foreground"
          >
            {{ $t('resource.task.instance.noInstanceHint') }}
            <Button
              v-access:code="['resource:task:add']"
              type="link"
              size="small"
              @click="handleOpenAddInstance(row)"
            >
              {{ $t('resource.task.instance.mountFirst') }}
            </Button>
          </div>

          <!-- 实例卡片横向自适应网格 -->
          <div
            v-else
            class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3"
          >
            <div
              v-for="inst in row.instances"
              :key="inst.instanceId"
              class="rounded-lg border border-border bg-card text-card-foreground p-3.5 shadow-2xs hover:border-primary/50 transition-all flex flex-col justify-between"
              :class="inst.enabled ? 'opacity-100' : 'opacity-70'"
            >
              <div>
                <!-- 卡片头部：算法与状态 -->
                <div class="flex items-start justify-between mb-2.5">
                  <div>
                    <div
                      class="font-medium text-sm text-foreground flex items-center gap-2"
                    >
                      <span>{{ inst.algorithmId }}</span>
                      <span
                        class="text-[10px] font-mono px-1 rounded bg-muted text-muted-foreground"
                      >
                        ID: {{ inst.instanceId.slice(0, 8) }}
                      </span>
                    </div>
                  </div>
                  <Tag :color="getInstanceStatusTag(inst.actualStatus).color">
                    {{ $t(getInstanceStatusTag(inst.actualStatus).textKey) }}
                  </Tag>
                </div>

                <!-- 性能仪表条：FPS 吞吐与目标 -->
                <div
                  class="bg-muted/40 rounded-md p-2.5 border border-border/60 mb-2.5 space-y-1.5 text-xs"
                >
                  <div class="flex justify-between items-center text-[11px]">
                    <span class="text-muted-foreground">{{
                      $t('resource.task.instance.inferenceFps')
                    }}</span>
                    <span class="font-mono font-medium">
                      <span
                        v-if="
                          inst.currentFps !== null &&
                          inst.currentFps !== undefined
                        "
                        :class="
                          inst.currentFps >= inst.analysisFps
                            ? 'text-emerald-600 dark:text-emerald-400 font-semibold'
                            : 'text-amber-600 dark:text-amber-400'
                        "
                      >
                        {{ Number(inst.currentFps).toFixed(1) }}
                      </span>
                      <span v-else>-</span>
                      <span class="text-muted-foreground">
                        / {{ inst.analysisFps }} FPS</span
                      >
                    </span>
                  </div>

                  <!-- FPS 进度条 -->
                  <Progress
                    :percent="
                      inst.analysisFps > 0
                        ? Math.min(
                            100,
                            ((inst.currentFps || 0) / inst.analysisFps) * 100,
                          )
                        : 0
                    "
                    :stroke-color="
                      inst.currentFps && inst.currentFps >= inst.analysisFps
                        ? '#10b981'
                        : '#f59e0b'
                    "
                    :show-info="false"
                    size="small"
                  />

                  <div
                    class="flex items-center justify-between text-[10px] text-muted-foreground pt-1 border-t border-border/40"
                  >
                    <span
                      >{{ $t('resource.task.instance.rulesCountLabel') }}:
                      <strong>{{ inst.rulesCount }}</strong></span
                    >
                    <span
                      >{{ $t('resource.task.instance.enableLabel') }}:
                      <strong>{{
                        inst.enabled
                          ? $t('resource.task.instance.statusEnabled')
                          : $t('resource.task.instance.statusDisabled')
                      }}</strong></span
                    >
                  </div>
                </div>

                <!-- 异常报错提示条 -->
                <div
                  v-if="inst.statusMessage"
                  class="mb-2 p-1.5 rounded bg-destructive/10 text-destructive border border-destructive/20 text-[11px] break-all"
                >
                  {{ inst.statusMessage }}
                </div>
              </div>

              <!-- 卡片底部操作 -->
              <div
                class="flex items-center justify-between pt-2 border-t border-border/60 text-xs"
              >
                <div class="flex items-center gap-1.5">
                  <span class="text-muted-foreground text-xs"
                    >{{ $t('resource.task.instance.enableLabel') }}:</span
                  >
                  <Switch
                    v-access:code="['resource:task:edit']"
                    :checked="inst.enabled"
                    size="small"
                    @change="
                      (val) => handleToggleInstanceEnabled(inst, Boolean(val))
                    "
                  />
                </div>
                <Space :size="4">
                  <Button
                    v-access:code="['resource:task:edit']"
                    type="link"
                    size="small"
                    class="p-0 text-xs"
                    @click="handleOpenEditInstance(row, inst)"
                  >
                    {{ $t('resource.task.instance.paramConfig') }}
                  </Button>
                  <span class="text-border">|</span>
                  <Popconfirm
                    :title="$t('resource.task.instance.deleteConfirm')"
                    @confirm="handleDeleteInstance(inst)"
                  >
                    <Button
                      v-access:code="['resource:task:delete']"
                      type="link"
                      danger
                      size="small"
                      class="p-0 text-xs"
                    >
                      {{ $t('resource.task.instance.remove') }}
                    </Button>
                  </Popconfirm>
                </Space>
              </div>
            </div>
          </div>
        </div>
      </template>
    </Grid>

    <!-- 任务创建/编辑 Modal -->
    <TaskFormModal
      v-model:open="formModalOpen"
      :task="currentEditTask"
      @success="handleFormSuccess"
    />

    <!-- 实例快速新建/编辑 Modal -->
    <InstanceFormModal
      v-model:open="instanceFormModalOpen"
      :camera-id="activeTargetCameraId"
      :instance="currentEditInstance"
      @success="handleFormSuccess"
    />

    <!-- 实例管理抽屉 -->
    <InstanceDrawer
      v-model:open="instanceDrawerOpen"
      :camera-id="activeDrawerCameraId"
      :task-name="activeDrawerTaskName"
      @change="gridApi.query()"
    />
  </Page>
</template>
