<script lang="ts" setup>
import type { CameraApi } from '#/api';

import { computed, onMounted, ref } from 'vue';

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
  Tag,
  Tooltip,
} from 'ant-design-vue';

import {
  batchDeleteCameraApi,
  deleteCameraApi,
  getCameraPageApi,
  probeCameraApi,
} from '#/api';

import CameraFormModal from './components/CameraFormModal.vue';
import CameraPreviewModal from './components/CameraPreviewModal.vue';

defineOptions({
  name: 'ResourceCamera',
});

// 数据与状态
const loading = ref(false);
const cameras = ref<CameraApi.CameraItem[]>([]);
const searchKeyword = ref('');
const filterStatus = ref<'all' | 'failed' | 'never' | 'success'>('all');

// 批量多选
const selectedCameraIds = ref<number[]>([]);
const batchOperating = ref(false);

// 全局巡检与单机测活
const inspectingAll = ref(false);
const probingIds = ref<Set<number>>(new Set());

// 弹窗状态
const formModalOpen = ref(false);
const currentEditCamera = ref<CameraApi.CameraItem | null>(null);

const previewModalOpen = ref(false);
const previewCamera = ref<CameraApi.CameraItem | null>(null);

// 获取列表数据
async function fetchCameras() {
  loading.value = true;
  try {
    const res = await getCameraPageApi({
      page: 1,
      pageSize: 100,
    });
    cameras.value = res.items || [];
  } catch (error) {
    console.error('Failed to fetch cameras:', error);
  } finally {
    loading.value = false;
  }
}

// 统计看板数据
const totalCount = computed(() => cameras.value.length);
const onlineCount = computed(
  () => cameras.value.filter((c) => c.lastProbeStatus === 'success').length,
);
const offlineCount = computed(
  () => cameras.value.filter((c) => c.lastProbeStatus === 'failed').length,
);
const untestedCount = computed(
  () => cameras.value.filter((c) => c.lastProbeStatus === 'never').length,
);

const connectivityRate = computed(() => {
  if (totalCount.value === 0) return 0;
  return Math.round((onlineCount.value / totalCount.value) * 100);
});

// 编码分布与网络协议统计
const dominantCodec = computed(() => {
  if (totalCount.value === 0) return '-';
  const h265 = cameras.value.filter(
    (c) =>
      c.lastCodec?.toLowerCase().includes('265') ||
      c.lastCodec?.toLowerCase().includes('hevc'),
  ).length;
  if (h265 > 0) {
    return `H.265 (${Math.round((h265 / totalCount.value) * 100)}%)`;
  }
  const h264 = cameras.value.filter((c) =>
    c.lastCodec?.toLowerCase().includes('264'),
  ).length;
  if (h264 > 0) {
    return `H.264 (${Math.round((h264 / totalCount.value) * 100)}%)`;
  }
  return 'H.264/H.265';
});

const tcpPolicyRate = computed(() => {
  if (totalCount.value === 0) return 0;
  const tcpCount = cameras.value.filter(
    (c) => c.lastSuccessTransport === 'tcp' || !c.lastSuccessTransport,
  ).length;
  return Math.round((tcpCount / totalCount.value) * 100);
});

const dualStreamCount = computed(
  () => cameras.value.filter((c) => Boolean(c.subRtspUrl)).length,
);

// 过滤后的摄像头列表
const filteredCameras = computed(() => {
  let result = [...cameras.value];

  // 状态过滤
  if (filterStatus.value !== 'all') {
    result = result.filter((c) => c.lastProbeStatus === filterStatus.value);
  }

  // 关键词过滤 (名称 / 编号 / RTSP)
  if (searchKeyword.value.trim()) {
    const kw = searchKeyword.value.trim().toLowerCase();
    result = result.filter(
      (c) =>
        c.name.toLowerCase().includes(kw) ||
        c.cameraId.toLowerCase().includes(kw) ||
        c.rtspUrl.toLowerCase().includes(kw),
    );
  }

  return result;
});

// 复制工具
function copyToClipboard(text: string) {
  navigator.clipboard
    ?.writeText(text)
    .then(() => message.success($t('resource.camera.copied')))
    .catch(() => undefined);
}

// 时间相对格式化
function formatRelativeTime(dateStr?: null | string): string {
  if (!dateStr) return '未测试';
  const time = new Date(dateStr).getTime();
  if (Number.isNaN(time)) return '未测试';
  const diff = Date.now() - time;
  if (diff < 1000) return '刚刚';
  if (diff < 60_000) return `${Math.floor(diff / 1000)}s 前`;
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}分钟前`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}小时前`;
  return `${Math.floor(diff / 86_400_000)}天前`;
}

// 错误码本地化
function failureCodeText(code: string): string {
  const key = `resource.camera.probeFailureCode.${code}` as const;
  const localized = $t(key);
  return localized === key ? code : localized;
}

// 右上角智能摄像头图腾视觉
function getProbeIconVisual(status: CameraApi.ProbeStatus) {
  switch (status) {
    case 'failed': {
      return 'border-rose-500/30 bg-rose-500/10 text-rose-500 hover:bg-rose-500/20 hover:border-rose-500/50 shadow-[0_0_12px_rgba(244,63,94,0.15)]';
    }
    case 'success': {
      return 'border-emerald-500/30 bg-emerald-500/10 text-emerald-500 hover:bg-emerald-500/20 hover:border-emerald-500/50 shadow-[0_0_12px_rgba(16,185,129,0.15)]';
    }
    default: {
      return 'border-border/70 bg-muted/40 text-muted-foreground/70 hover:bg-muted/70 hover:text-foreground';
    }
  }
}

// 状态小圆点
function getStatusDotClass(status: CameraApi.ProbeStatus) {
  switch (status) {
    case 'failed': {
      return 'bg-rose-500 shadow-[0_0_6px_rgba(244,63,94,0.8)]';
    }
    case 'success': {
      return 'bg-emerald-500 shadow-[0_0_6px_rgba(16,185,129,0.8)]';
    }
    default: {
      return 'bg-slate-400';
    }
  }
}

// 单设备测活
async function handleSingleProbe(row: CameraApi.CameraItem) {
  if (probingIds.value.has(row.id)) return;
  probingIds.value.add(row.id);
  const target = cameras.value.find((c) => c.id === row.id);
  try {
    const result = await probeCameraApi({
      id: row.id,
      protocol: 'rtsp',
      rtspUrl: row.rtspUrl,
    });
    if (result.status === 'success') {
      message.success(`${row.name}: ${$t('resource.camera.probeSuccessDesc')}`);
    } else {
      message.error(
        `${row.name}: ${$t('resource.camera.probeFailedDesc')} (${failureCodeText(result.failureCode)})`,
      );
    }
    // 原位局部刷新
    if (target) {
      target.lastProbeStatus = result.status;
      target.lastProbeAt = new Date().toISOString();
      if (result.status === 'success') {
        target.lastCodec = result.codec;
        target.lastWidth = result.width;
        target.lastHeight = result.height;
        target.lastFps = result.fps;
        target.lastSuccessTransport = result.selectedTransport;
        target.lastProbeErrorCode = '';
      } else {
        target.lastProbeErrorCode = result.failureCode || 'RTSP_CONNECT_FAILED';
      }
    }
  } catch (error: any) {
    console.error(error);
    // 关键修复：请求超时或异常失败时，必须将卡片状态标记为 failed，并更新时间与错误码，绝不能让卡片保留历史的"连通正常"假象！
    if (target) {
      target.lastProbeStatus = 'failed';
      target.lastProbeAt = new Date().toISOString();
      const isTimeout =
        error?.code === 'ECONNABORTED' ||
        error?.message?.includes('timeout') ||
        error?.message?.includes('超时');
      target.lastProbeErrorCode = isTimeout
        ? 'RTSP_PLAY_TIMEOUT'
        : 'RTSP_CONNECT_FAILED';
    }
    message.error(`${row.name}: ${$t('resource.camera.probeFailed')}`);
  } finally {
    probingIds.value.delete(row.id);
  }
}

// 全局一键巡检
async function handleInspectAll() {
  if (inspectingAll.value || cameras.value.length === 0) return;
  inspectingAll.value = true;
  message.loading({
    content: $t('resource.camera.inspectingAll'),
    key: 'inspect-all',
  });
  try {
    await Promise.allSettled(
      cameras.value.map(async (c) => {
        try {
          const res = await probeCameraApi({
            id: c.id,
            protocol: 'rtsp',
            rtspUrl: c.rtspUrl,
          });
          c.lastProbeStatus = res.status;
          c.lastProbeAt = new Date().toISOString();
          if (res.status === 'success') {
            c.lastCodec = res.codec;
            c.lastWidth = res.width;
            c.lastHeight = res.height;
            c.lastFps = res.fps;
            c.lastSuccessTransport = res.selectedTransport;
            c.lastProbeErrorCode = '';
          } else {
            c.lastProbeErrorCode = res.failureCode || 'RTSP_CONNECT_FAILED';
          }
        } catch (error: any) {
          c.lastProbeStatus = 'failed';
          c.lastProbeAt = new Date().toISOString();
          const isTimeout =
            error?.code === 'ECONNABORTED' ||
            error?.message?.includes('timeout') ||
            error?.message?.includes('超时');
          c.lastProbeErrorCode = isTimeout
            ? 'RTSP_PLAY_TIMEOUT'
            : 'RTSP_CONNECT_FAILED';
        }
      }),
    );
    message.success({
      content: $t('resource.camera.inspectSuccess'),
      key: 'inspect-all',
    });
  } finally {
    inspectingAll.value = false;
  }
}

// 多选交互
function toggleSelect(id: number) {
  if (selectedCameraIds.value.includes(id)) {
    selectedCameraIds.value = selectedCameraIds.value.filter(
      (item) => item !== id,
    );
  } else {
    selectedCameraIds.value.push(id);
  }
}

function clearSelection() {
  selectedCameraIds.value = [];
}

// 批量测活
async function handleBatchProbe() {
  if (selectedCameraIds.value.length === 0) return;
  batchOperating.value = true;
  message.loading({
    content: $t('resource.camera.batchProbing'),
    key: 'batch-probe',
  });
  try {
    const targets = cameras.value.filter((c) =>
      selectedCameraIds.value.includes(c.id),
    );
    await Promise.allSettled(
      targets.map(async (c) => {
        try {
          const res = await probeCameraApi({
            id: c.id,
            protocol: 'rtsp',
            rtspUrl: c.rtspUrl,
          });
          c.lastProbeStatus = res.status;
          c.lastProbeAt = new Date().toISOString();
          if (res.status === 'success') {
            c.lastCodec = res.codec;
            c.lastWidth = res.width;
            c.lastHeight = res.height;
            c.lastFps = res.fps;
            c.lastSuccessTransport = res.selectedTransport;
            c.lastProbeErrorCode = '';
          } else {
            c.lastProbeErrorCode = res.failureCode || 'RTSP_CONNECT_FAILED';
          }
        } catch (error: any) {
          c.lastProbeStatus = 'failed';
          c.lastProbeAt = new Date().toISOString();
          const isTimeout =
            error?.code === 'ECONNABORTED' ||
            error?.message?.includes('timeout') ||
            error?.message?.includes('超时');
          c.lastProbeErrorCode = isTimeout
            ? 'RTSP_PLAY_TIMEOUT'
            : 'RTSP_CONNECT_FAILED';
        }
      }),
    );
    message.success({
      content: $t('system.common.success'),
      key: 'batch-probe',
    });
  } finally {
    batchOperating.value = false;
  }
}

// 批量删除
async function handleBatchDelete() {
  if (selectedCameraIds.value.length === 0) return;
  batchOperating.value = true;
  try {
    await batchDeleteCameraApi(selectedCameraIds.value);
    message.success($t('system.common.success'));
    clearSelection();
    await fetchCameras();
  } catch (error) {
    console.error(error);
  } finally {
    batchOperating.value = false;
  }
}

// 单机删除
async function handleDeleteCamera(camera: CameraApi.CameraItem) {
  try {
    await deleteCameraApi(camera.id);
    message.success($t('system.common.success'));
    selectedCameraIds.value = selectedCameraIds.value.filter(
      (id) => id !== camera.id,
    );
    await fetchCameras();
  } catch (error) {
    console.error(error);
  }
}

// 快速抽检实时画面
function handleOpenPreview(camera: CameraApi.CameraItem) {
  previewCamera.value = camera;
  previewModalOpen.value = true;
}

// 新建与编辑
function handleOpenCreate() {
  currentEditCamera.value = null;
  formModalOpen.value = true;
}

function handleOpenEdit(camera: CameraApi.CameraItem) {
  currentEditCamera.value = camera;
  formModalOpen.value = true;
}

onMounted(() => {
  fetchCameras();
});
</script>

<template>
  <Page auto-content-height content-class="p-4 flex flex-col gap-4">
    <!-- 顶部全局资产看板 (Edge Camera Hub: 层次分明、微光克制) -->
    <div
      class="rounded-2xl border border-border/70 bg-card p-4 shadow-sm backdrop-blur-sm"
    >
      <div
        class="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between"
      >
        <!-- 左侧：核心宏观资产维度 -->
        <div class="flex flex-wrap items-center gap-6">
          <!-- 连通态总览 -->
          <div class="flex items-center gap-3">
            <div
              class="relative flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-emerald-500/30 bg-emerald-500/10 text-emerald-500"
            >
              <IconifyIcon icon="lucide:cctv" class="text-xl" />
              <span
                v-if="onlineCount > 0"
                class="absolute -right-0.5 -top-0.5 h-2.5 w-2.5 rounded-full bg-emerald-500 ring-2 ring-card shadow-[0_0_8px_rgba(16,185,129,0.8)] animate-pulse"
              ></span>
            </div>
            <div>
              <div class="flex items-baseline gap-1.5">
                <span
                  class="font-mono text-xl font-bold tracking-tight text-foreground"
                >
                  {{ onlineCount }} / {{ totalCount }}
                </span>
                <span class="text-xs font-medium text-muted-foreground">
                  {{ $t('resource.camera.onlineCount') }}
                </span>
                <Tag
                  color="success"
                  class="m-0 ml-1 rounded text-[10px] font-mono px-1 py-0 font-medium"
                >
                  {{ connectivityRate }}%
                </Tag>
              </div>
              <div class="mt-0.5 flex items-center gap-2 text-xs">
                <button
                  v-if="offlineCount > 0"
                  type="button"
                  class="text-rose-500 hover:underline flex items-center gap-1 font-medium text-[11px] cursor-pointer"
                  :title="$t('resource.camera.filterByFault')"
                  @click="filterStatus = 'failed'"
                >
                  <span
                    class="h-1.5 w-1.5 rounded-full bg-rose-500 animate-pulse"
                  ></span>
                  <span>{{ offlineCount }}
                    {{ $t('resource.camera.offlineCount') }} (点击排查)</span>
                </button>
                <span v-else class="text-muted-foreground text-[11px]">
                  全域链路在线
                </span>
                <span
                  v-if="untestedCount > 0"
                  class="text-muted-foreground text-[11px]"
                >
                  · {{ untestedCount }} 未测试
                </span>
              </div>
            </div>
          </div>

          <!-- 规格分布概览 -->
          <div
            class="hidden sm:flex items-center gap-2 pl-4 border-l border-border/60 text-xs font-mono"
          >
            <div class="flex flex-col">
              <span class="text-[10px] text-muted-foreground font-sans">主编解码与策略</span>
              <div class="mt-0.5 flex items-center gap-2">
                <span class="font-bold text-foreground">{{
                  dominantCodec
                }}</span>
                <span class="text-muted-foreground/60">·</span>
                <span class="text-foreground">TCP {{ tcpPolicyRate }}%</span>
                <span class="text-muted-foreground/60">·</span>
                <span
                  class="text-emerald-600 dark:text-emerald-400 font-semibold"
                  >{{ dualStreamCount }} 双码流</span>
              </div>
            </div>
          </div>
        </div>

        <!-- 右侧：紧凑工具栏与操作入口 -->
        <div class="flex flex-wrap items-center gap-2.5">
          <!-- 搜索输入框 -->
          <div class="w-48 sm:w-56">
            <Input
              v-model:value="searchKeyword"
              allow-clear
              size="middle"
              :placeholder="$t('resource.camera.searchPlaceholder')"
              class="rounded-xl border-border/80 text-xs"
            >
              <template #prefix>
                <IconifyIcon
                  icon="lucide:search"
                  class="text-muted-foreground text-xs"
                />
              </template>
            </Input>
          </div>

          <!-- 状态筛选胶囊群 -->
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
              全部 ({{ totalCount }})
            </button>
            <button
              type="button"
              class="rounded-lg px-2.5 py-1 text-xs font-medium transition-all"
              :class="
                filterStatus === 'success'
                  ? 'bg-card text-emerald-500 shadow-sm font-semibold'
                  : 'text-muted-foreground hover:text-foreground'
              "
              @click="filterStatus = 'success'"
            >
              在线 ({{ onlineCount }})
            </button>
            <button
              type="button"
              class="rounded-lg px-2.5 py-1 text-xs font-medium transition-all"
              :class="
                filterStatus === 'failed'
                  ? 'bg-card text-rose-500 shadow-sm font-semibold'
                  : 'text-muted-foreground hover:text-foreground'
              "
              @click="filterStatus = 'failed'"
            >
              异常 ({{ offlineCount }})
            </button>
          </div>

          <!-- 全局一键巡检 -->
          <Button
            size="middle"
            class="rounded-xl text-xs border-border/80"
            :loading="inspectingAll"
            v-access:code="['resource:camera:probe']"
            @click="handleInspectAll"
          >
            <template #icon>
              <IconifyIcon icon="lucide:refresh-cw" class="text-xs" />
            </template>
            {{ $t('resource.camera.inspectAll') }}
          </Button>

          <!-- 新增摄像头主按钮 -->
          <Button
            type="primary"
            size="middle"
            class="rounded-xl text-xs bg-emerald-600 hover:bg-emerald-500 border-none shadow-sm"
            v-access:code="['resource:camera:add']"
            @click="handleOpenCreate"
          >
            <template #icon>
              <IconifyIcon icon="lucide:plus" class="text-xs" />
            </template>
            {{ $t('resource.camera.add') }}
          </Button>
        </div>
      </div>
    </div>

    <!-- 核心 Bento 卡片矩阵 (Edge Camera Bento Matrix: 极致清爽、主次分明) -->
    <div class="relative min-h-[300px] flex-1">
      <Spin :spinning="loading" tip="正在载入摄像头矩阵...">
        <div
          v-if="filteredCameras.length > 0"
          class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-4 pb-20"
        >
          <div
            v-for="cam in filteredCameras"
            :key="cam.id"
            class="group relative flex flex-col justify-between rounded-2xl border bg-card p-4 transition-all duration-200 hover:-translate-y-0.5 hover:shadow-lg"
            :class="
              selectedCameraIds.includes(cam.id)
                ? 'border-emerald-500/70 bg-emerald-950/5 ring-1 ring-emerald-500/50'
                : 'border-border/70 hover:border-slate-400/50'
            "
          >
            <!-- 勾选复选框 (左上角悬浮显现) -->
            <button
              type="button"
              class="absolute left-3 top-3 z-10 flex h-5 w-5 items-center justify-center rounded-full border transition-all cursor-pointer"
              :class="
                selectedCameraIds.includes(cam.id)
                  ? 'border-emerald-500 bg-emerald-500 text-white shadow-sm opacity-100'
                  : 'border-border/80 bg-card/90 text-transparent opacity-0 group-hover:opacity-100 hover:border-emerald-500'
              "
              title="勾选"
              @click.stop="toggleSelect(cam.id)"
            >
              <IconifyIcon icon="lucide:check" class="text-xs" />
            </button>

            <!-- 主内容区 -->
            <div>
              <!-- 1. 卡片头部：主身份（名称独占主行，高对比度加粗）+ 次级编号 + 右侧智能摄像头图腾与菜单 -->
              <div class="flex items-start justify-between gap-3 pl-4">
                <div class="flex-1 min-w-0">
                  <!-- 主标题：摄像头名称 -->
                  <div class="flex items-center gap-2">
                    <span
                      class="h-2.5 w-2.5 rounded-full shrink-0"
                      :class="getStatusDotClass(cam.lastProbeStatus)"
                    ></span>
                    <Tooltip :title="cam.name">
                      <h3
                        class="truncate text-base font-bold text-foreground group-hover:text-emerald-500 transition-colors"
                      >
                        {{ cam.name || '未命名摄像头' }}
                      </h3>
                    </Tooltip>
                  </div>

                  <!-- 次级行：芯片编号 + 传输协议与连通状态 (高对比度、不被挤压) + 独立备注 -->
                  <div class="mt-1.5 flex items-center gap-2 text-xs">
                    <!-- 芯片编号徽章 -->
                    <span
                      class="rounded bg-muted/80 px-1.5 py-0.5 font-mono text-[10px] text-foreground/80 font-medium select-all shrink-0"
                    >
                      {{ cam.cameraId }}
                    </span>

                    <!-- 连通态与传输协议 (独立展示，带 shrink-0 绝不挤丢) -->
                    <span
                      v-if="cam.lastProbeStatus === 'success'"
                      class="inline-flex items-center gap-1 font-mono text-[11px] font-semibold text-emerald-600 dark:text-emerald-400 shrink-0"
                    >
                      <span
                        class="h-1.5 w-1.5 rounded-full bg-emerald-500"
                      ></span>
                      <span>{{
                        cam.lastSuccessTransport
                          ? `${cam.lastSuccessTransport.toUpperCase()} 连通`
                          : '在线'
                      }}</span>
                    </span>
                    <span
                      v-else-if="cam.lastProbeStatus === 'failed'"
                      class="inline-flex items-center gap-1 text-[11px] font-semibold text-rose-500 shrink-0"
                    >
                      <span class="h-1.5 w-1.5 rounded-full bg-rose-500"></span>
                      <span>{{ failureCodeText(cam.lastProbeErrorCode) }}</span>
                    </span>
                    <span
                      v-else
                      class="inline-flex items-center gap-1 text-[11px] text-slate-400 shrink-0 font-mono"
                    >
                      <span
                        class="h-1.5 w-1.5 rounded-full bg-slate-400"
                      ></span>
                      <span>待测试</span>
                    </span>

                    <!-- 独立备注 (不与连通状态互斥) -->
                    <span
                      v-if="cam.remark"
                      class="text-[11px] text-muted-foreground/70 truncate min-w-0"
                      :title="cam.remark"
                    >
                      · {{ cam.remark }}
                    </span>
                  </div>
                </div>

                <!-- 右上角智能摄像头图腾徽章 + 更多操作 -->
                <div class="flex items-center gap-1.5 shrink-0 pt-0.5">
                  <!-- 智能摄像头图腾 (点击直接打开实时流抽检) -->
                  <Tooltip :title="$t('resource.camera.spotCheck')">
                    <button
                      type="button"
                      class="group/badge relative flex h-8 w-8 items-center justify-center rounded-xl border transition-all duration-200 hover:scale-105 cursor-pointer"
                      :class="getProbeIconVisual(cam.lastProbeStatus)"
                      @click.stop="handleOpenPreview(cam)"
                    >
                      <IconifyIcon
                        icon="lucide:cctv"
                        class="text-base transition-transform group-hover/badge:scale-110"
                      />
                      <!-- 在线微光脉冲点 -->
                      <span
                        v-if="cam.lastProbeStatus === 'success'"
                        class="absolute -right-0.5 -top-0.5 h-1.5 w-1.5 rounded-full bg-emerald-500 ring-2 ring-card shadow-[0_0_6px_rgba(16,185,129,0.9)] animate-pulse"
                      ></span>
                    </button>
                  </Tooltip>

                  <!-- 更多操作菜单 -->
                  <Dropdown :trigger="['click']" placement="bottomRight">
                    <button
                      type="button"
                      class="h-8 w-8 rounded-xl flex items-center justify-center text-muted-foreground hover:bg-muted hover:text-foreground transition-colors"
                    >
                      <IconifyIcon
                        icon="lucide:more-vertical"
                        class="text-xs"
                      />
                    </button>
                    <template #overlay>
                      <Menu class="text-xs">
                        <MenuItem key="preview" @click="handleOpenPreview(cam)">
                          <div class="flex items-center gap-2">
                            <IconifyIcon
                              icon="lucide:tv"
                              class="text-xs text-emerald-500"
                            />
                            <span>{{ $t('resource.camera.spotCheck') }}</span>
                          </div>
                        </MenuItem>
                        <MenuItem
                          key="edit"
                          v-access:code="['resource:camera:edit']"
                          @click="handleOpenEdit(cam)"
                        >
                          <div class="flex items-center gap-2">
                            <IconifyIcon icon="lucide:edit-2" class="text-xs" />
                            <span>{{ $t('system.common.edit') }}</span>
                          </div>
                        </MenuItem>
                        <MenuItem
                          key="copyUrl"
                          @click="copyToClipboard(cam.rtspUrl)"
                        >
                          <div class="flex items-center gap-2">
                            <IconifyIcon icon="lucide:copy" class="text-xs" />
                            <span>{{ $t('resource.camera.copyUrl') }}</span>
                          </div>
                        </MenuItem>
                        <MenuItem
                          key="copyId"
                          @click="copyToClipboard(cam.cameraId)"
                        >
                          <div class="flex items-center gap-2">
                            <IconifyIcon icon="lucide:hash" class="text-xs" />
                            <span>{{ $t('resource.camera.copyId') }}</span>
                          </div>
                        </MenuItem>
                        <MenuItem
                          key="delete"
                          v-access:code="['resource:camera:delete']"
                        >
                          <Popconfirm
                            :title="$t('system.common.confirmDelete')"
                            @confirm="handleDeleteCamera(cam)"
                          >
                            <div class="flex items-center gap-2 text-rose-500">
                              <IconifyIcon
                                icon="lucide:trash-2"
                                class="text-xs"
                              />
                              <span>{{ $t('system.common.delete') }}</span>
                            </div>
                          </Popconfirm>
                        </MenuItem>
                      </Menu>
                    </template>
                  </Dropdown>
                </div>
              </div>

              <!-- 2. 中部数据层：媒体硬件规格 + 物理网络接入节点 (视觉降噪) -->
              <div class="my-3 space-y-2">
                <!-- 硬件媒体规格条 (Codec · 分辨率 · 帧率 · 双码流) -->
                <div
                  class="flex items-center justify-between rounded-xl bg-muted/25 border border-border/50 px-3 py-2 text-xs font-mono"
                >
                  <div class="flex items-center gap-2 text-foreground">
                    <span class="font-bold text-foreground">
                      {{ cam.lastCodec || 'H.264' }}
                    </span>
                    <span class="text-muted-foreground/50">·</span>
                    <span>
                      {{
                        cam.lastWidth > 0
                          ? `${cam.lastWidth}×${cam.lastHeight}`
                          : '1080P'
                      }}
                    </span>
                    <span class="text-muted-foreground/50">·</span>
                    <span
                      class="font-bold text-emerald-600 dark:text-emerald-400"
                    >
                      {{ cam.lastFps > 0 ? `${cam.lastFps}fps` : '25fps' }}
                    </span>
                  </div>

                  <Tag
                    v-if="cam.subRtspUrl"
                    color="blue"
                    class="m-0 text-[9px] px-1 py-0 font-mono"
                  >
                    双码流
                  </Tag>
                  <span
                    v-else
                    class="text-[10px] text-muted-foreground font-mono"
                  >
                    单码流
                  </span>
                </div>

                <!-- 视频流地址 (完整清晰展示 + 右侧一键复制) -->
                <div
                  class="flex items-center justify-between rounded-xl bg-card border border-border/70 px-3 py-2 text-xs font-mono"
                >
                  <div
                    class="flex items-center gap-1.5 text-muted-foreground truncate min-w-0 pr-2"
                  >
                    <IconifyIcon
                      icon="lucide:link"
                      class="text-xs shrink-0 text-slate-400"
                    />
                    <Tooltip :title="cam.rtspUrl">
                      <span
                        class="truncate select-all text-foreground/90 font-mono"
                      >
                        {{ cam.rtspUrl }}
                      </span>
                    </Tooltip>
                  </div>

                  <button
                    type="button"
                    class="shrink-0 p-1 text-muted-foreground hover:text-foreground transition-colors rounded hover:bg-muted cursor-pointer"
                    :title="$t('resource.camera.copyUrl')"
                    @click="copyToClipboard(cam.rtspUrl)"
                  >
                    <IconifyIcon icon="lucide:copy" class="text-xs" />
                  </button>
                </div>
              </div>
            </div>

            <!-- 3. 底栏动作区：测活心跳时间 + 原位唯一高频动作 [测试连接] -->
            <div
              class="border-t border-border/60 pt-2.5 flex items-center justify-between text-xs font-mono"
            >
              <div
                class="flex items-center gap-1.5 text-muted-foreground text-[11px]"
              >
                <span
                  v-if="cam.lastProbeStatus === 'success'"
                  class="text-emerald-500 font-bold flex items-center gap-1"
                >
                  <IconifyIcon icon="lucide:zap" class="text-[10px]" />
                  <span>连通正常</span>
                </span>
                <span
                  v-else-if="cam.lastProbeStatus === 'failed'"
                  class="text-rose-500 font-bold flex items-center gap-1"
                >
                  <IconifyIcon
                    icon="lucide:alert-triangle"
                    class="text-[10px]"
                  />
                  <span>链路中断</span>
                </span>
                <span v-else class="text-slate-400"> 未测试 </span>
                <span>·</span>
                <span>{{ formatRelativeTime(cam.lastProbeAt) }}</span>
              </div>

              <!-- 唯一高频就地动作：测活 -->
              <button
                type="button"
                class="flex items-center gap-1 px-2.5 py-1 rounded-lg border border-border/70 hover:border-emerald-500/60 bg-muted/30 hover:bg-emerald-500/10 text-muted-foreground hover:text-emerald-500 text-xs transition-all cursor-pointer font-sans"
                :disabled="probingIds.has(cam.id)"
                v-access:code="['resource:camera:probe']"
                @click="handleSingleProbe(cam)"
              >
                <IconifyIcon
                  icon="lucide:activity"
                  class="text-xs"
                  :class="{ 'animate-spin': probingIds.has(cam.id) }"
                />
                <span>{{
                  probingIds.has(cam.id)
                    ? '测活中...'
                    : $t('resource.camera.probe')
                }}</span>
              </button>
            </div>
          </div>
        </div>

        <!-- 工业科技感空状态 (Zero State) -->
        <div
          v-else-if="!loading"
          class="flex flex-col items-center justify-center rounded-2xl border border-dashed border-border/80 bg-card/40 py-20 text-center"
        >
          <div
            class="mb-3 flex h-16 w-16 items-center justify-center rounded-2xl border border-emerald-500/20 bg-emerald-500/10 text-emerald-500 shadow-inner"
          >
            <IconifyIcon icon="lucide:cctv" class="text-3xl animate-pulse" />
          </div>
          <h4 class="text-base font-bold text-foreground">
            {{ $t('resource.camera.emptyTitle') }}
          </h4>
          <p class="mt-1 max-w-sm text-xs text-muted-foreground">
            {{ $t('resource.camera.emptyDesc') }}
          </p>
          <Button
            type="primary"
            size="middle"
            class="mt-4 rounded-xl text-xs bg-emerald-600 hover:bg-emerald-500 border-none"
            v-access:code="['resource:camera:add']"
            @click="handleOpenCreate"
          >
            <template #icon>
              <IconifyIcon icon="lucide:plus" class="text-xs" />
            </template>
            {{ $t('resource.camera.emptyAddBtn') }}
          </Button>
        </div>
      </Spin>

      <!-- 底部悬浮磨砂玻璃操作坞 (Floating Glass Action Dock) -->
      <transition
        enter-active-class="transition duration-200 ease-out"
        enter-from-class="translate-y-12 opacity-0"
        enter-to-class="translate-y-0 opacity-100"
        leave-active-class="transition duration-150 ease-in"
        leave-from-class="translate-y-0 opacity-100"
        leave-to-class="translate-y-12 opacity-0"
      >
        <div
          v-if="selectedCameraIds.length > 0"
          class="fixed bottom-6 left-1/2 -translate-x-1/2 z-30 flex items-center gap-3 px-4 py-2.5 rounded-full bg-slate-900/90 dark:bg-slate-900/95 backdrop-blur-md border border-slate-700/80 shadow-2xl text-slate-100 text-xs"
        >
          <div class="flex items-center gap-2 pr-2 border-r border-slate-700">
            <span
              class="h-2 w-2 rounded-full bg-emerald-400 animate-pulse"
            ></span>
            <span class="font-medium text-slate-200">
              {{
                $t('resource.camera.selectedCount', {
                  count: selectedCameraIds.length,
                })
              }}
            </span>
          </div>

          <div class="flex items-center gap-2">
            <!-- 批量测活 -->
            <Button
              size="small"
              class="bg-emerald-600 hover:bg-emerald-500 border-none text-xs text-white"
              :loading="batchOperating"
              v-access:code="['resource:camera:probe']"
              @click="handleBatchProbe"
            >
              <template #icon>
                <IconifyIcon icon="lucide:activity" class="text-xs" />
              </template>
              {{ $t('resource.camera.batchProbe') }}
            </Button>

            <!-- 批量删除 -->
            <Popconfirm
              :title="
                $t('resource.camera.batchDeleteConfirm', {
                  count: selectedCameraIds.length,
                })
              "
              @confirm="handleBatchDelete"
            >
              <Button
                danger
                size="small"
                class="text-xs"
                :loading="batchOperating"
                v-access:code="['resource:camera:delete']"
              >
                <template #icon>
                  <IconifyIcon icon="lucide:trash-2" class="text-xs" />
                </template>
                {{ $t('system.common.batchDelete') }}
              </Button>
            </Popconfirm>

            <!-- 取消选择 -->
            <button
              type="button"
              class="ml-1 p-1 rounded hover:bg-slate-800 text-slate-400 hover:text-slate-200 transition-colors cursor-pointer"
              title="取消选择"
              @click="clearSelection"
            >
              <IconifyIcon icon="lucide:x" class="text-xs" />
            </button>
          </div>
        </div>
      </transition>
    </div>

    <!-- 新建/编辑摄像头弹窗 (支持主流安防厂商预设模板与握手诊断) -->
    <CameraFormModal
      v-model:open="formModalOpen"
      :camera="currentEditCamera"
      @success="fetchCameras"
      @preview="handleOpenPreview"
    />

    <!-- 按需轻量实时画面抽检视窗 (HUD Modal，关闭自动释放流) -->
    <CameraPreviewModal
      v-model:open="previewModalOpen"
      :camera="previewCamera"
    />
  </Page>
</template>
