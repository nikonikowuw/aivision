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
  Progress,
  Space,
  Switch,
  Tag,
  Tooltip,
} from 'ant-design-vue';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  deleteInstanceApi,
  deleteTaskApi,
  getInstanceListApi,
  getTaskListApi,
  setInstanceEnabledApi,
  setTaskEnabledApi,
  type TaskApi,
} from '#/api';

import InstanceDrawer from './components/InstanceDrawer.vue';
import InstanceFormModal from './components/InstanceFormModal.vue';
import TaskFormModal from './components/TaskFormModal.vue';

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

function getTaskStatusTag(status: TaskApi.TaskStatusCode) {
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

function getInstanceStatusTag(status: number) {
  switch (status) {
    case 1: {
      return { color: 'processing', text: '启动中' };
    }
    case 2: {
      return { color: 'success', text: '正常' };
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
  columns: [
    {
      type: 'expand',
      width: 48,
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
      title: '算法实例流水线',
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
      width: 160,
      align: 'center',
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

        // 检查是否有任务或实例处于 STARTING 状态
        const hasStarting = res.items?.some(
          (t) =>
            t.actualStatus === 1 ||
            t.instances?.some((i) => i.actualStatus === 1),
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
      message.error('未找到对应实例详情');
    }
  } catch (err: any) {
    message.error(err.message || '获取实例详情失败');
  }
}

async function handleToggleInstanceEnabled(
  brief: TaskApi.TaskInstanceBrief,
  checked: boolean,
) {
  try {
    await setInstanceEnabledApi(brief.instanceId, checked);
    message.success($t('system.common.success'));
    gridApi.reload();
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
    <Grid>
      <template #toolbar-tools>
        <Button type="primary" @click="handleAdd">
          {{ $t('resource.task.add') }}
        </Button>
      </template>

      <!-- 任务名称 + 摄像头信息 -->
      <template #taskName="{ row }">
        <div class="py-1">
          <div class="font-medium text-foreground text-sm flex items-center gap-1.5">
            <span>{{ row.name }}</span>
            <span
              v-if="row.instanceCount !== undefined && row.instanceCount > 0"
              class="text-[10px] px-1.5 py-0.5 rounded-full bg-primary/10 text-primary font-mono font-normal"
            >
              {{ row.instanceCount }} 实例
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
          :checked="row.desiredEnabled"
          size="small"
          @change="(val) => handleToggleEnabled(row, Boolean(val))"
        />
      </template>

      <!-- 任务运行状态 Tag -->
      <template #actualStatus="{ row }">
        <Tooltip v-if="row.statusMessage" :title="row.statusMessage">
          <Tag :color="getTaskStatusTag(row.actualStatus).color">
            {{ getTaskStatusTag(row.actualStatus).text }} ⓘ
          </Tag>
        </Tooltip>
        <Tag v-else :color="getTaskStatusTag(row.actualStatus).color">
          {{ getTaskStatusTag(row.actualStatus).text }}
        </Tag>
      </template>

      <!-- 算法实例流水线（紧凑管道胶囊槽位） -->
      <template #instances="{ row }">
        <div
          v-if="!row.instances || row.instances.length === 0"
          class="flex items-center gap-2 py-1 text-xs text-muted-foreground"
        >
          <span class="italic">未配置算法实例</span>
          <Button
            type="link"
            size="small"
            class="p-0 text-xs"
            @click="handleOpenAddInstance(row)"
          >
            + 立即挂载
          </Button>
        </div>

        <div v-else class="flex flex-wrap items-center gap-2 py-1">
          <div
            v-for="inst in row.instances"
            :key="inst.instanceId"
            class="flex items-center gap-2 px-2.5 py-1 rounded-md border border-border bg-card shadow-2xs hover:border-primary/60 transition-all cursor-pointer"
            :class="inst.enabled ? 'opacity-100' : 'opacity-60'"
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
              <span class="text-[10px] text-muted-foreground font-mono mt-0.5">
                <span
                  v-if="inst.currentFps !== null && inst.currentFps !== undefined"
                  :class="inst.currentFps >= inst.analysisFps ? 'text-emerald-600 dark:text-emerald-400 font-semibold' : 'text-amber-600 dark:text-amber-400'"
                >
                  {{ Number(inst.currentFps).toFixed(1) }}
                </span>
                <span v-else>-</span>
                <span class="opacity-60"> / </span>
                <span>{{ inst.analysisFps }} FPS</span>
              </span>
            </div>

            <!-- 轻量启停开关 -->
            <Switch
              :checked="inst.enabled"
              size="small"
              class="scale-85 origin-right"
              @click.stop
              @change="(val) => handleToggleInstanceEnabled(inst, Boolean(val))"
            />
          </div>

          <!-- 快捷添加实例按钮 -->
          <Button
            type="dashed"
            size="small"
            class="h-7 px-2 text-xs flex items-center gap-1 text-muted-foreground hover:text-primary"
            @click="handleOpenAddInstance(row)"
          >
            + 挂载
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
        <Space :size="8">
          <Button type="link" size="small" @click="handleOpenDrawer(row)">
            实例抽屉
          </Button>
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

      <!-- 展开行：算法实例控制台 (Instance Console) -->
      <template #expandContent="{ row }">
        <div class="px-6 py-4 bg-muted/25 border-y border-border">
          <!-- 展开控制台头部 -->
          <div class="flex items-center justify-between mb-3">
            <div class="flex items-center gap-2">
              <span class="text-xs font-semibold text-foreground tracking-wide">
                挂载的算法实例控制台 ({{ row.instances?.length || 0 }})
              </span>
              <span class="text-xs text-muted-foreground">
                · 通道 {{ row.cameraId }} 共享单路 RTSP 拉流与 VPU 硬件解码
              </span>
            </div>
            <div class="flex items-center gap-2">
              <Button
                size="small"
                type="primary"
                ghost
                @click="handleOpenAddInstance(row)"
              >
                + 挂载新算法实例
              </Button>
              <Button size="small" @click="handleOpenDrawer(row)">
                进入抽屉管理
              </Button>
            </div>
          </div>

          <!-- 实例为空提示 -->
          <div
            v-if="!row.instances || row.instances.length === 0"
            class="p-6 rounded-lg border border-dashed border-border bg-card text-center text-xs text-muted-foreground"
          >
            该任务尚未挂载任何算法实例，无法在边缘端执行推理分析。
            <Button
              type="link"
              size="small"
              @click="handleOpenAddInstance(row)"
            >
              立即挂载首个算法
            </Button>
          </div>

          <!-- 实例卡片横向自适应网格 -->
          <div v-else class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
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
                    <div class="font-medium text-sm text-foreground flex items-center gap-2">
                      <span>{{ inst.algorithmId }}</span>
                      <span class="text-[10px] font-mono px-1 rounded bg-muted text-muted-foreground">
                        ID: {{ inst.instanceId.slice(0, 8) }}
                      </span>
                    </div>
                  </div>
                  <Tag :color="getInstanceStatusTag(inst.actualStatus).color">
                    {{ getInstanceStatusTag(inst.actualStatus).text }}
                  </Tag>
                </div>

                <!-- 性能仪表条：FPS 吞吐与目标 -->
                <div class="bg-muted/40 rounded-md p-2.5 border border-border/60 mb-2.5 space-y-1.5 text-xs">
                  <div class="flex justify-between items-center text-[11px]">
                    <span class="text-muted-foreground">推理帧率 (FPS)</span>
                    <span class="font-mono font-medium">
                      <span
                        v-if="inst.currentFps !== null && inst.currentFps !== undefined"
                        :class="inst.currentFps >= inst.analysisFps ? 'text-emerald-600 dark:text-emerald-400 font-semibold' : 'text-amber-600 dark:text-amber-400'"
                      >
                        {{ Number(inst.currentFps).toFixed(1) }}
                      </span>
                      <span v-else>-</span>
                      <span class="text-muted-foreground"> / {{ inst.analysisFps }} FPS</span>
                    </span>
                  </div>

                  <!-- FPS 进度条 -->
                  <Progress
                    :percent="inst.analysisFps > 0 ? Math.min(100, ((inst.currentFps || 0) / inst.analysisFps) * 100) : 0"
                    :stroke-color="inst.currentFps && inst.currentFps >= inst.analysisFps ? '#10b981' : '#f59e0b'"
                    :show-info="false"
                    size="small"
                  />

                  <div class="flex items-center justify-between text-[10px] text-muted-foreground pt-1 border-t border-border/40">
                    <span>检测规则: <strong>{{ inst.rulesCount }} 处</strong></span>
                    <span>状态: <strong>{{ inst.enabled ? '已启用' : '已停用' }}</strong></span>
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
              <div class="flex items-center justify-between pt-2 border-t border-border/60 text-xs">
                <div class="flex items-center gap-1.5">
                  <span class="text-muted-foreground text-xs">使能:</span>
                  <Switch
                    :checked="inst.enabled"
                    size="small"
                    @change="(val) => handleToggleInstanceEnabled(inst, Boolean(val))"
                  />
                </div>
                <Space :size="4">
                  <Button
                    type="link"
                    size="small"
                    class="p-0 text-xs"
                    @click="handleOpenEditInstance(row, inst)"
                  >
                    参数配置
                  </Button>
                  <span class="text-border">|</span>
                  <Popconfirm
                    :title="$t('resource.task.instance.deleteConfirm')"
                    @confirm="handleDeleteInstance(inst)"
                  >
                    <Button
                      type="link"
                      danger
                      size="small"
                      class="p-0 text-xs"
                    >
                      移除
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
      @change="gridApi.reload()"
    />
  </Page>
</template>
