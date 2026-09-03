<script lang="ts" setup>
import type { TaskApi } from '#/api';

import {
  computed,
  onActivated,
  onDeactivated,
  onMounted,
  onUnmounted,
  ref,
  watch,
} from 'vue';

import { Page } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';
import { $t } from '@vben/locales';

import {
  Button,
  Dropdown,
  Input,
  Menu,
  MenuItem,
  message,
  Popconfirm,
  Spin,
  Switch,
  Tag,
  Tooltip,
} from 'ant-design-vue';

import {
  batchDeleteTasksApi,
  deleteTaskApi,
  getInstanceListApi,
  getTaskListApi,
  getTaskStatsApi,
  setTaskEnabledApi,
} from '#/api';
import { formatAlgorithmName } from '#/utils/i18n';

import InstanceDrawer from './components/InstanceDrawer.vue';
import InstanceFormModal from './components/InstanceFormModal.vue';
import TaskFormModal from './components/TaskFormModal.vue';

defineOptions({
  name: 'ResourceTask',
});

// 任务列表与检索过滤
const loading = ref(false);
const tasks = ref<TaskApi.TaskItem[]>([]);
const searchKeyword = ref('');
const filterStatus = ref<'all' | 'running' | 'stopped' | 'unconfigured'>('all');

// 批量多选
const selectedTaskIds = ref<string[]>([]);
const batchOperating = ref(false);

// 弹窗与抽屉状态
const formModalOpen = ref(false);
const currentEditTask = ref<null | TaskApi.TaskItem>(null);

const instanceDrawerOpen = ref(false);
const activeDrawerCameraId = ref('');
const activeDrawerTaskName = ref('');

const instanceFormModalOpen = ref(false);
const activeTargetCameraId = ref('');
const currentEditInstance = ref<null | TaskApi.InstanceItem>(null);

// 统计看板数据
const taskStats = ref<null | TaskApi.TaskStats>(null);

async function loadStats() {
  try {
    taskStats.value = await getTaskStatsApi();
  } catch {
    // 拦截器已统一报错
  }
}

const loadPercent = computed(() => {
  const s = taskStats.value;
  if (!s || s.totalUnits <= 0) return 0;
  return Math.min(100, Math.round((s.usedUnits / s.totalUnits) * 100));
});

// 静默防抖轮询机制
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
    void loadStats();
    const res = await getTaskListApi({
      page: 1,
      pageSize: 100,
    });

    if (!hasActiveRunningOrStarting(res.items)) {
      stopPolling();
    }

    if (tasks.value.length > 0 && res.items?.length > 0) {
      const freshMap = new Map(res.items.map((item) => [item.cameraId, item]));
      for (const row of tasks.value) {
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

async function fetchTasks() {
  loading.value = true;
  try {
    await loadStats();
    const res = await getTaskListApi({
      page: 1,
      pageSize: 100,
    });
    tasks.value = res.items || [];
    if (hasActiveRunningOrStarting(tasks.value)) {
      startPolling();
    } else {
      stopPolling();
    }
  } catch {
    message.error('加载视频分析任务列表失败');
  } finally {
    loading.value = false;
  }
}

async function handleVisibilityChange() {
  if (document.hidden) {
    stopPolling();
  } else if (isViewActive) {
    await fetchTasks();
  }
}

onActivated(() => {
  isViewActive = true;
  void fetchTasks();
});

onDeactivated(() => {
  isViewActive = false;
  stopPolling();
});

onMounted(() => {
  void fetchTasks();
  document.addEventListener('visibilitychange', handleVisibilityChange);
});

onUnmounted(() => {
  document.removeEventListener('visibilitychange', handleVisibilityChange);
  stopPolling();
});

watch(instanceDrawerOpen, (isOpen) => {
  if (isOpen) {
    stopPolling();
  } else if (isViewActive && hasActiveRunningOrStarting(tasks.value)) {
    startPolling();
  }
});

// 过滤计算后的任务清单
const filteredTasks = computed(() => {
  let result = tasks.value;
  const kw = searchKeyword.value.trim().toLowerCase();
  if (kw) {
    result = result.filter(
      (t) =>
        t.name.toLowerCase().includes(kw) ||
        t.cameraId.toLowerCase().includes(kw) ||
        t.instances?.some(
          (i) =>
            i.algorithmId.toLowerCase().includes(kw) ||
            formatAlgorithmName(i.algorithmId).toLowerCase().includes(kw),
        ),
    );
  }

  if (filterStatus.value === 'running') {
    result = result.filter((t) => t.actualStatus === 1 || t.actualStatus === 2);
  } else if (filterStatus.value === 'stopped') {
    result = result.filter((t) => t.actualStatus === 5);
  } else if (filterStatus.value === 'unconfigured') {
    result = result.filter((t) => !t.instances || t.instances.length === 0);
  }

  return result;
});

// 算法视觉标签与图标提取
function getAlgoVisual(algoId: string) {
  const id = algoId.toLowerCase();
  if (id.includes('yolo') || id.includes('detect')) {
    return {
      bg: 'border-blue-500/30 bg-blue-500/10 text-blue-500',
      icon: 'lucide:scan',
      label: '目标检测',
      tagColor: 'blue',
    };
  }
  if (id.includes('face')) {
    return {
      bg: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-500',
      icon: 'lucide:user-check',
      label: '人脸感知',
      tagColor: 'green',
    };
  }
  if (id.includes('fire') || id.includes('smoke')) {
    return {
      bg: 'border-amber-500/30 bg-amber-500/10 text-amber-500',
      icon: 'lucide:flame',
      label: '烟火识别',
      tagColor: 'warning',
    };
  }
  if (id.includes('plate') || id.includes('traffic') || id.includes('car')) {
    return {
      bg: 'border-purple-500/30 bg-purple-500/10 text-purple-500',
      icon: 'lucide:car',
      label: '车牌识别',
      tagColor: 'purple',
    };
  }
  return {
    bg: 'border-cyan-500/30 bg-cyan-500/10 text-cyan-500',
    icon: 'lucide:cpu',
    label: '边缘算子',
    tagColor: 'cyan',
  };
}

// 状态小圆点与文本
function getTaskStatusDotClass(status: number) {
  switch (status) {
    case 1: {
      return 'bg-blue-500 animate-pulse';
    }
    case 2: {
      return 'bg-emerald-500 shadow-[0_0_8px_rgba(16,185,129,0.8)]';
    }
    case 3: {
      return 'bg-amber-500';
    }
    case 4: {
      return 'bg-orange-500 animate-pulse';
    }
    case 5: {
      return 'bg-slate-400';
    }
    case 6: {
      return 'bg-rose-500 shadow-[0_0_8px_rgba(244,63,94,0.8)]';
    }
    default: {
      return 'bg-slate-400';
    }
  }
}

function getStatusText(status: number) {
  switch (status) {
    case 1: {
      return '正在启动';
    }
    case 2: {
      return '实时分析中';
    }
    case 3: {
      return '降级运行';
    }
    case 4: {
      return '网络重连';
    }
    case 5: {
      return '已停用';
    }
    case 6: {
      return '运行异常';
    }
    default: {
      return '未初始化';
    }
  }
}

// 单任务操作
function openCreateModal() {
  currentEditTask.value = null;
  formModalOpen.value = true;
}

function openEditTaskModal(task: TaskApi.TaskItem) {
  currentEditTask.value = task;
  formModalOpen.value = true;
}

async function handleDeleteTask(task: TaskApi.TaskItem) {
  try {
    await deleteTaskApi(task.cameraId);
    message.success($t('system.common.success'));
    await fetchTasks();
  } catch {
    // 统一报错
  }
}

async function handleToggleTaskEnabled(
  task: TaskApi.TaskItem,
  checked: boolean,
) {
  task.desiredEnabled = checked;
  try {
    await setTaskEnabledApi(task.cameraId, checked);
    message.success($t('system.common.success'));
    startPolling();
  } catch {
    task.desiredEnabled = !checked;
  }
}

function openWorkbench(task: TaskApi.TaskItem) {
  activeDrawerCameraId.value = task.cameraId;
  activeDrawerTaskName.value = task.name;
  instanceDrawerOpen.value = true;
}

function openMountModal(task: TaskApi.TaskItem) {
  activeTargetCameraId.value = task.cameraId;
  currentEditInstance.value = null;
  instanceFormModalOpen.value = true;
}

async function openEditInstanceModal(
  task: TaskApi.TaskItem,
  brief: TaskApi.TaskInstanceBrief,
) {
  activeTargetCameraId.value = task.cameraId;
  try {
    const list = await getInstanceListApi(task.cameraId);
    const target = (list || []).find(
      (i: TaskApi.InstanceItem) => i.instanceId === brief.instanceId,
    );
    if (target) {
      currentEditInstance.value = target;
      instanceFormModalOpen.value = true;
    } else {
      message.error('未找到对应实例详情');
    }
  } catch {
    message.error('获取实例详情失败');
  }
}

// 多选与批量操作
function toggleSelect(cameraId: string) {
  if (selectedTaskIds.value.includes(cameraId)) {
    selectedTaskIds.value = selectedTaskIds.value.filter(
      (id) => id !== cameraId,
    );
  } else {
    selectedTaskIds.value.push(cameraId);
  }
}

function clearSelection() {
  selectedTaskIds.value = [];
}

async function handleBatchStart() {
  batchOperating.value = true;
  try {
    await Promise.all(
      selectedTaskIds.value.map((id) => setTaskEnabledApi(id, true)),
    );
    message.success($t('system.common.success'));
    await fetchTasks();
  } finally {
    batchOperating.value = false;
  }
}

async function handleBatchStop() {
  batchOperating.value = true;
  try {
    await Promise.all(
      selectedTaskIds.value.map((id) => setTaskEnabledApi(id, false)),
    );
    message.success($t('system.common.success'));
    await fetchTasks();
  } finally {
    batchOperating.value = false;
  }
}

async function handleBatchDelete() {
  batchOperating.value = true;
  try {
    await batchDeleteTasksApi({ cameraIds: selectedTaskIds.value });
    message.success($t('system.common.success'));
    selectedTaskIds.value = [];
    await fetchTasks();
  } finally {
    batchOperating.value = false;
  }
}

function getTaskTotalFps(task: TaskApi.TaskItem): number {
  if (!task.instances || task.instances.length === 0) return 0;
  return task.instances.reduce((acc, inst) => acc + (inst.currentFps || 0), 0);
}

function formatRelativeTime(dateStr?: null | string): string {
  if (!dateStr) return '暂无数据';
  const time = new Date(dateStr).getTime();
  if (Number.isNaN(time)) return '暂无数据';
  const diff = Date.now() - time;
  if (diff < 1000) return '刚刚';
  if (diff < 60_000) return `${(diff / 1000).toFixed(0)}s 前`;
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}分钟前`;
  return `${Math.floor(diff / 3_600_000)}小时前`;
}
</script>

<template>
  <Page auto-content-height>
    <div class="space-y-4 pb-20">
      <!-- 1. 顶部全局概览看板 (Edge AI Hub: 与 Camera 看板高度一致的高级质感) -->
      <div
        class="rounded-2xl border border-border/70 bg-card p-4 shadow-sm backdrop-blur-sm"
      >
        <div
          class="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between"
        >
          <!-- 左侧：3 个宏观分析维度看板 -->
          <div class="flex flex-wrap items-center gap-6">
            <!-- 维度 1: 通道在线率 -->
            <div class="flex items-center gap-3">
              <div
                class="relative flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-emerald-500/30 bg-emerald-500/10 text-emerald-500"
              >
                <IconifyIcon icon="lucide:video" class="text-xl" />
                <span
                  v-if="(taskStats?.runningTasks ?? 0) > 0"
                  class="absolute -right-0.5 -top-0.5 h-2.5 w-2.5 rounded-full bg-emerald-500 ring-2 ring-card shadow-[0_0_8px_rgba(16,185,129,0.8)] animate-pulse"
                ></span>
              </div>
              <div>
                <div class="flex items-baseline gap-1.5">
                  <span
                    class="font-mono text-xl font-bold tracking-tight text-foreground"
                  >
                    {{ taskStats?.runningTasks ?? 0 }} /
                    {{ taskStats?.totalTasks ?? 0 }}
                  </span>
                  <span class="text-xs font-medium text-muted-foreground">通道在线</span>
                  <Tag
                    color="success"
                    class="m-0 ml-1 rounded text-[10px] font-mono px-1 py-0 font-medium"
                  >
                    {{
                      taskStats?.totalTasks
                        ? Math.round(
                            ((taskStats.runningTasks ?? 0) /
                              taskStats.totalTasks) *
                              100,
                          )
                        : 0
                    }}%
                  </Tag>
                </div>
                <div
                  class="mt-0.5 flex items-center gap-2 text-xs text-muted-foreground"
                >
                  <span
                    class="text-emerald-600 dark:text-emerald-400 font-medium"
                  >
                    {{ taskStats?.runningTasks ?? 0 }} 路实时分析中
                  </span>
                  <span class="text-muted-foreground/40">·</span>
                  <span>{{
                      (taskStats?.totalTasks ?? 0) -
                      (taskStats?.runningTasks ?? 0)
                    }}
                    路就绪</span>
                </div>
              </div>
            </div>

            <!-- 维度 2: 活跃算法实例群 -->
            <div
              class="hidden sm:flex items-center gap-3 pl-4 border-l border-border/60"
            >
              <div
                class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-blue-500/30 bg-blue-500/10 text-blue-500"
              >
                <IconifyIcon icon="lucide:cpu" class="text-xl" />
              </div>
              <div>
                <div class="flex items-baseline gap-1.5">
                  <span
                    class="font-mono text-xl font-bold tracking-tight text-foreground"
                  >
                    {{ taskStats?.enabledInstances ?? 0 }}
                  </span>
                  <span class="text-xs font-medium text-muted-foreground">活跃算子</span>
                  <span class="text-xs text-muted-foreground font-mono">/ {{ taskStats?.totalInstances ?? 0 }} 总数</span>
                </div>
                <div class="mt-0.5 text-xs text-muted-foreground">
                  多模型流水线并行调度
                </div>
              </div>
            </div>

            <!-- 维度 3: 边缘 NPU 算力负荷与配额 -->
            <div
              class="hidden md:flex items-center gap-3 pl-4 border-l border-border/60"
            >
              <div
                class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-amber-500/30 bg-amber-500/10 text-amber-500"
              >
                <IconifyIcon icon="lucide:zap" class="text-xl" />
              </div>
              <div class="w-36">
                <div class="flex items-baseline justify-between">
                  <span class="text-xs font-medium text-muted-foreground">NPU 算力负荷</span>
                  <span class="font-mono text-xs font-bold text-foreground">{{ loadPercent }}%</span>
                </div>
                <div
                  class="mt-1 h-1.5 w-full rounded-full bg-muted overflow-hidden"
                >
                  <div
                    class="h-full rounded-full transition-all duration-500"
                    :class="
                      loadPercent > 85
                        ? 'bg-rose-500'
                        : loadPercent > 65
                          ? 'bg-amber-500'
                          : 'bg-emerald-500'
                    "
                    :style="{ width: `${loadPercent}%` }"
                  ></div>
                </div>
                <div
                  class="mt-1 flex justify-between text-[10px] text-muted-foreground font-mono"
                >
                  <span>{{ taskStats?.usedUnits ?? 0 }} Units</span>
                  <span>余
                    {{
                      (taskStats?.totalUnits ?? 64) -
                      (taskStats?.usedUnits ?? 0)
                    }}</span>
                </div>
              </div>
            </div>
          </div>

          <!-- 右侧：搜索输入框、过滤胶囊与新建按钮 -->
          <div class="flex flex-wrap items-center gap-2.5">
            <Input
              v-model:value="searchKeyword"
              allow-clear
              placeholder="搜索任务或通道..."
              class="w-44 sm:w-52 rounded-xl text-xs"
              size="middle"
            >
              <template #prefix>
                <IconifyIcon
                  icon="lucide:search"
                  class="text-muted-foreground text-xs"
                />
              </template>
            </Input>

            <div
              class="flex items-center rounded-xl bg-muted/40 p-1 border border-border/60 text-xs"
            >
              <button
                type="button"
                class="rounded-lg px-2.5 py-1 text-xs font-medium transition-all"
                :class="
                  filterStatus === 'all'
                    ? 'bg-card text-foreground shadow-sm font-semibold'
                    : 'text-muted-foreground hover:text-foreground'
                "
                @click="filterStatus = 'all'"
              >
                全部 ({{ tasks.length }})
              </button>
              <button
                type="button"
                class="rounded-lg px-2.5 py-1 text-xs font-medium transition-all"
                :class="
                  filterStatus === 'running'
                    ? 'bg-card text-emerald-500 shadow-sm font-semibold'
                    : 'text-muted-foreground hover:text-foreground'
                "
                @click="filterStatus = 'running'"
              >
                运行中
              </button>
              <button
                type="button"
                class="rounded-lg px-2.5 py-1 text-xs font-medium transition-all"
                :class="
                  filterStatus === 'stopped'
                    ? 'bg-card text-foreground shadow-sm font-semibold'
                    : 'text-muted-foreground hover:text-foreground'
                "
                @click="filterStatus = 'stopped'"
              >
                已停止
              </button>
              <button
                type="button"
                class="rounded-lg px-2.5 py-1 text-xs font-medium transition-all"
                :class="
                  filterStatus === 'unconfigured'
                    ? 'bg-card text-amber-500 shadow-sm font-semibold'
                    : 'text-muted-foreground hover:text-foreground'
                "
                @click="filterStatus = 'unconfigured'"
              >
                未配置
              </button>
            </div>

            <Button
              v-access:code="['resource:task:add']"
              type="primary"
              size="middle"
              class="rounded-xl text-xs bg-emerald-600 hover:bg-emerald-500 border-none shadow-sm flex items-center gap-1"
              @click="openCreateModal"
            >
              <template #icon>
                <IconifyIcon icon="lucide:plus" class="text-xs" />
              </template>
              <span>新建任务</span>
            </Button>
          </div>
        </div>
      </div>

      <!-- 2. Bento 卡片矩阵 (Edge AI Appliance: 质感硬朗、层次鲜明) -->
      <div class="relative min-h-[300px]">
        <Spin :spinning="loading" tip="正在载入视频分析流水线...">
          <div
            v-if="filteredTasks.length > 0"
            class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-4"
          >
            <div
              v-for="task in filteredTasks"
              :key="task.cameraId"
              class="group relative flex flex-col justify-between rounded-2xl border bg-card p-4 transition-all duration-200 hover:-translate-y-0.5 hover:shadow-lg"
              :class="[
                selectedTaskIds.includes(task.cameraId)
                  ? 'border-emerald-500/70 bg-emerald-950/5 ring-1 ring-emerald-500/50'
                  : 'border-border/70 hover:border-slate-400/50',
              ]"
            >
              <!-- 勾选复选框 (左上角悬浮显现) -->
              <button
                type="button"
                class="absolute left-3 top-3 z-10 flex h-5 w-5 items-center justify-center rounded-full border transition-all cursor-pointer"
                :class="
                  selectedTaskIds.includes(task.cameraId)
                    ? 'border-emerald-500 bg-emerald-500 text-white shadow-sm opacity-100'
                    : 'border-border/80 bg-card/90 text-transparent opacity-0 group-hover:opacity-100 hover:border-emerald-500'
                "
                title="勾选"
                @click.stop="toggleSelect(task.cameraId)"
              >
                <IconifyIcon icon="lucide:check" class="text-xs" />
              </button>

              <!-- 卡片内容主体 -->
              <div>
                <!-- 头部：主身份、状态点、通道号、启停 Switch、快捷菜单 -->
                <div class="flex items-start justify-between gap-2.5 pl-4">
                  <div class="flex-1 min-w-0">
                    <div class="flex items-center gap-2">
                      <span
                        class="h-2.5 w-2.5 rounded-full shrink-0"
                        :class="getTaskStatusDotClass(task.actualStatus)"
                      ></span>
                      <Tooltip :title="task.name">
                        <h3
                          class="truncate text-sm sm:text-base font-bold text-foreground group-hover:text-emerald-600 dark:group-hover:text-emerald-400 transition-colors cursor-pointer"
                          @click="openWorkbench(task)"
                        >
                          {{ task.name || '未命名分析任务' }}
                        </h3>
                      </Tooltip>
                    </div>

                    <div
                      class="mt-1 flex flex-wrap items-center gap-1.5 text-[11px] text-muted-foreground font-mono"
                    >
                      <span
                        class="rounded bg-muted/80 px-1.5 py-0.5 text-foreground/90 font-medium select-all shrink-0"
                      >
                        {{ task.cameraId }}
                      </span>
                      <span
                        class="text-muted-foreground/80 font-sans whitespace-nowrap"
                      >
                        {{ getStatusText(task.actualStatus) }}
                      </span>
                      <span class="text-muted-foreground/40">·</span>
                      <span class="whitespace-nowrap">{{
                        formatRelativeTime(task.lastFrameAt || task.reportedAt)
                      }}</span>
                    </div>
                  </div>

                  <!-- 右上角：启停开关 + 更多下拉菜单 -->
                  <div class="flex items-center gap-1.5 shrink-0 pt-0.5">
                    <Switch
                      v-access:code="['resource:task:edit']"
                      :checked="task.desiredEnabled"
                      size="small"
                      @change="
                        (val) => handleToggleTaskEnabled(task, Boolean(val))
                      "
                    />

                    <Dropdown :trigger="['click']" placement="bottomRight">
                      <button
                        type="button"
                        class="h-7 w-7 rounded-lg flex items-center justify-center text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
                      >
                        <IconifyIcon
                          icon="lucide:more-vertical"
                          class="text-xs"
                        />
                      </button>
                      <template #overlay>
                        <Menu class="text-xs">
                          <MenuItem @click="openWorkbench(task)">
                            <div class="flex items-center gap-2">
                              <IconifyIcon
                                icon="lucide:external-link"
                                class="text-xs text-emerald-500"
                              />
                              <span>进入工作台</span>
                            </div>
                          </MenuItem>
                          <MenuItem @click="openEditTaskModal(task)">
                            <div class="flex items-center gap-2">
                              <IconifyIcon
                                icon="lucide:edit-2"
                                class="text-xs"
                              />
                              <span>重命名任务</span>
                            </div>
                          </MenuItem>
                          <MenuItem>
                            <Popconfirm
                              :title="$t('resource.task.deleteConfirm')"
                              @confirm="handleDeleteTask(task)"
                            >
                              <div
                                class="flex items-center gap-2 text-rose-500"
                              >
                                <IconifyIcon
                                  icon="lucide:trash-2"
                                  class="text-xs"
                                />
                                <span>删除通道</span>
                              </div>
                            </Popconfirm>
                          </MenuItem>
                        </Menu>
                      </template>
                    </Dropdown>
                  </div>
                </div>

                <!-- 性能与算力指标条 (自适应折行防挤压) -->
                <div
                  class="my-3 flex flex-wrap items-center justify-between gap-2 rounded-xl bg-muted/30 border border-border/50 px-3 py-2 text-xs font-mono"
                >
                  <div
                    class="flex flex-wrap items-center gap-x-2 gap-y-1 text-foreground"
                  >
                    <span
                      class="flex items-center gap-1 text-muted-foreground font-sans whitespace-nowrap"
                    >
                      <IconifyIcon
                        icon="lucide:activity"
                        class="text-xs text-emerald-500"
                      />
                      <span>吞吐</span>
                    </span>
                    <span
                      class="font-bold text-emerald-600 dark:text-emerald-400 whitespace-nowrap"
                    >
                      {{ getTaskTotalFps(task).toFixed(1) }} fps
                    </span>
                    <span class="text-muted-foreground/40">·</span>
                    <span
                      class="text-muted-foreground font-sans whitespace-nowrap"
                      >算力</span>
                    <span
                      class="font-bold text-amber-600 dark:text-amber-400 whitespace-nowrap"
                    >
                      {{ (task.instances?.length || 0) * 8 }} Units
                    </span>
                  </div>

                  <Tag
                    :color="
                      task.actualStatus === 2
                        ? 'success'
                        : task.actualStatus === 1
                          ? 'processing'
                          : 'default'
                    "
                    class="m-0 text-[10px] px-1.5 py-0 font-mono shrink-0"
                  >
                    {{
                      task.actualStatus === 2
                        ? 'RUNNING'
                        : task.actualStatus === 1
                          ? 'STARTING'
                          : 'IDLE'
                    }}
                  </Tag>
                </div>

                <!-- AI 算法流水线算子卡片列 -->
                <div class="space-y-1.5">
                  <div
                    class="flex items-center justify-between text-[11px] text-muted-foreground px-0.5"
                  >
                    <span class="font-medium flex items-center gap-1">
                      <IconifyIcon
                        icon="lucide:layers"
                        class="text-xs text-emerald-500"
                      />
                      <span>已挂载算法流水线</span>
                    </span>
                    <span class="font-mono text-[10px]">
                      {{
                        task.instances?.filter((i) => i.enabled).length || 0
                      }}/{{ task.instances?.length || 0 }} 活跃
                    </span>
                  </div>

                  <!-- 算法未挂载时的引导插槽 -->
                  <div
                    v-if="!task.instances || task.instances.length === 0"
                    class="rounded-xl border border-dashed border-border/80 bg-muted/20 p-3 text-center"
                  >
                    <p class="text-xs text-muted-foreground">
                      该通道暂未挂载任何推理算法
                    </p>
                    <button
                      type="button"
                      class="mt-1.5 text-xs text-emerald-600 dark:text-emerald-400 hover:underline font-medium inline-flex items-center gap-1 cursor-pointer"
                      @click="openMountModal(task)"
                    >
                      <IconifyIcon icon="lucide:plus" class="text-xs" />
                      <span>立即挂载首个算法</span>
                    </button>
                  </div>

                  <!-- 算法实例行卡片 (响应式弹性结构，小屏不重叠) -->
                  <div
                    v-for="inst in task.instances"
                    :key="inst.instanceId"
                    class="rounded-xl border bg-card/60 px-2.5 py-2 flex flex-wrap sm:flex-nowrap items-center justify-between gap-2 transition-all hover:bg-muted/40 cursor-pointer"
                    :class="
                      inst.enabled
                        ? 'border-border/70'
                        : 'border-border/40 opacity-60'
                    "
                    @click="openEditInstanceModal(task, inst)"
                  >
                    <div class="flex items-center gap-2 min-w-0 flex-1">
                      <div
                        class="flex h-6 w-6 items-center justify-center rounded-lg border text-xs shrink-0"
                        :class="getAlgoVisual(inst.algorithmId).bg"
                      >
                        <IconifyIcon
                          :icon="getAlgoVisual(inst.algorithmId).icon"
                        />
                      </div>
                      <div class="min-w-0 flex-1">
                        <div class="flex items-center gap-1.5 flex-wrap">
                          <span
                            class="text-xs font-semibold text-foreground truncate max-w-[110px] sm:max-w-[140px]"
                          >
                            {{ formatAlgorithmName(inst.algorithmId) }}
                          </span>
                          <Tag
                            :color="getAlgoVisual(inst.algorithmId).tagColor"
                            class="m-0 text-[9px] px-1 py-0 scale-90 shrink-0"
                          >
                            {{ getAlgoVisual(inst.algorithmId).label }}
                          </Tag>
                        </div>
                      </div>
                    </div>

                    <div
                      class="flex items-center gap-2 shrink-0 font-mono text-xs ml-auto sm:ml-0"
                    >
                      <span
                        class="text-muted-foreground text-[11px] whitespace-nowrap"
                      >
                        <strong class="text-foreground">{{
                          (inst.currentFps ?? 0).toFixed(0)
                        }}</strong>/{{ inst.analysisFps }}fps
                      </span>
                      <span
                        class="text-[10px] bg-muted px-1.5 py-0.5 rounded text-muted-foreground whitespace-nowrap"
                      >
                        {{ inst.rulesCount ?? 0 }} 规则
                      </span>
                    </div>
                  </div>

                  <!-- 挂载更多算法插槽按钮 -->
                  <button
                    v-if="task.instances && task.instances.length > 0"
                    type="button"
                    class="w-full py-1.5 rounded-xl border border-dashed border-border/80 hover:border-emerald-500/60 hover:bg-emerald-500/5 text-muted-foreground hover:text-emerald-500 text-xs font-medium flex items-center justify-center gap-1 transition-all cursor-pointer"
                    @click="openMountModal(task)"
                  >
                    <IconifyIcon icon="lucide:plus" class="text-xs" />
                    <span>挂载算法</span>
                  </button>
                </div>
              </div>

              <!-- 卡片底部：状态文本与工作台按钮 -->
              <div
                class="mt-3 pt-3 border-t border-border/60 flex items-center justify-between gap-2"
              >
                <span class="text-xs text-muted-foreground font-mono truncate">
                  {{
                    task.instances?.length
                      ? `${task.instances.length} 个模型已配置`
                      : '等待算法挂载'
                  }}
                </span>

                <Button
                  type="primary"
                  ghost
                  size="small"
                  class="rounded-lg text-xs flex items-center gap-1 text-emerald-600 border-emerald-500 hover:bg-emerald-50 dark:hover:bg-emerald-950/30 shrink-0"
                  @click="openWorkbench(task)"
                >
                  <span>工作台</span>
                  <IconifyIcon icon="lucide:arrow-right" class="text-xs" />
                </Button>
              </div>
            </div>
          </div>

          <!-- 极简空状态 -->
          <div
            v-if="filteredTasks.length === 0 && !loading"
            class="rounded-2xl border border-border/80 bg-card p-12 text-center space-y-3"
          >
            <div
              class="size-12 rounded-2xl bg-muted text-muted-foreground mx-auto flex items-center justify-center text-xl"
            >
              <IconifyIcon icon="lucide:video-off" />
            </div>
            <div>
              <h3 class="text-sm font-semibold text-foreground">
                暂无视频分析任务
              </h3>
              <p class="text-xs text-muted-foreground mt-1">
                从接入的摄像头源创建首条边缘 AI 分析流水线
              </p>
            </div>
            <Button
              type="primary"
              size="small"
              class="bg-emerald-600 hover:bg-emerald-500 border-none"
              @click="openCreateModal"
            >
              ＋ 新建分析任务
            </Button>
          </div>
        </Spin>
      </div>

      <!-- 3. 悬浮玻璃拟态批量操作坞 (Floating Glass Action Dock) -->
      <div
        v-if="selectedTaskIds.length > 0"
        class="fixed bottom-6 left-1/2 -translate-x-1/2 z-40 px-5 py-2.5 rounded-full bg-slate-900/95 dark:bg-zinc-900/95 text-white border border-slate-700/80 dark:border-zinc-700/80 shadow-2xl backdrop-blur-md flex items-center gap-4 text-xs"
      >
        <span class="font-medium flex items-center gap-1.5">
          <IconifyIcon
            icon="lucide:sparkles"
            class="text-emerald-400 size-3.5"
          />
          <span>已选
            <strong class="font-mono text-emerald-400 font-bold">{{
              selectedTaskIds.length
            }}</strong>
            路任务</span>
        </span>

        <div class="h-3 w-px bg-slate-700 dark:bg-zinc-700"></div>

        <div class="flex items-center gap-2">
          <Button
            type="primary"
            size="small"
            class="bg-emerald-600 hover:bg-emerald-500 border-none text-xs h-7"
            :loading="batchOperating"
            @click="handleBatchStart"
          >
            批量启动
          </Button>

          <Button
            size="small"
            class="bg-slate-800 hover:bg-slate-700 text-slate-200 border-slate-700 text-xs h-7"
            :loading="batchOperating"
            @click="handleBatchStop"
          >
            批量停用
          </Button>

          <Popconfirm
            title="确定要批量删除选中的分析任务吗？此操作不可逆。"
            @confirm="handleBatchDelete"
          >
            <Button
              danger
              size="small"
              class="text-xs h-7"
              :loading="batchOperating"
            >
              批量删除
            </Button>
          </Popconfirm>

          <button
            type="button"
            class="p-1 rounded hover:bg-slate-800 text-slate-400 hover:text-slate-200 transition-colors ml-1"
            title="取消选择"
            @click="clearSelection"
          >
            <IconifyIcon icon="lucide:x" class="size-3.5" />
          </button>
        </div>
      </div>
    </div>

    <!-- 任务创建与编辑弹窗 (含可视化视频源卡片选择) -->
    <TaskFormModal
      v-model:open="formModalOpen"
      :task="currentEditTask"
      @success="fetchTasks"
    />

    <!-- 深度工作台抽屉 (三栏 IDE 架构，实时流与几何防区画布) -->
    <InstanceDrawer
      v-model:open="instanceDrawerOpen"
      :camera-id="activeDrawerCameraId"
      :task-name="activeDrawerTaskName"
      @change="fetchTasks"
    />

    <!-- 算法模型挂载与参数调优弹窗 (左右分栏模型集市) -->
    <InstanceFormModal
      v-model:open="instanceFormModalOpen"
      :camera-id="activeTargetCameraId"
      :instance="currentEditInstance"
      @success="fetchTasks"
    />
  </Page>
</template>
