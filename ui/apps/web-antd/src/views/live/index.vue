<script setup lang="ts">
import type { CameraApi } from '#/api/core/camera';

import { computed, onBeforeUnmount, onMounted, ref } from 'vue';

import { Page } from '@vben/common-ui';
import { Grid, Search, Square } from '@vben/icons';
import { $t } from '@vben/locales';

import {
  getCameraPageApi,
  startLivePreviewApi,
  stopLivePreviewApi,
} from '#/api/core/camera';
import VideoPlayer from '#/components/video/VideoPlayer.vue';

type SplitMode = 1 | 4 | 9;

interface PlayerSlot {
  index: number;
  camera: CameraApi.CameraItem | null;
  streamUrl: string;
  streamType: 'main' | 'sub';
}

const splitMode = ref<SplitMode>(1);
const selectedSlotIndex = ref<number>(0);
const lastAssignedIndex = ref<number>(-1);
const cameraList = ref<CameraApi.CameraItem[]>([]);
const searchKeyword = ref('');
const loading = ref(false);

// 拖拽状态
const draggingCamera = ref<CameraApi.CameraItem | null>(null);
const draggingSlotIndex = ref<null | number>(null);
const dragOverSlotIndex = ref<null | number>(null);

const slots = ref<PlayerSlot[]>(
  Array.from({ length: 9 }, (_, i) => ({
    index: i,
    camera: null,
    streamUrl: '',
    streamType: 'main',
  })),
);

const visibleSlots = computed(() => {
  return slots.value.slice(0, splitMode.value);
});

const filteredCameras = computed(() => {
  if (!searchKeyword.value) return cameraList.value;
  const kw = searchKeyword.value.toLowerCase();
  return cameraList.value.filter((c) => c.name.toLowerCase().includes(kw));
});

const gridClass = computed(() => {
  switch (splitMode.value) {
    case 1: {
      return 'grid-cols-1 grid-rows-1';
    }
    case 4: {
      return 'grid-cols-2 grid-rows-2';
    }
    case 9: {
      return 'grid-cols-3 grid-rows-3';
    }
    default: {
      return 'grid-cols-1 grid-rows-1';
    }
  }
});

async function fetchCameras() {
  loading.value = true;
  try {
    const res = await getCameraPageApi({ page: 1, pageSize: 100 });
    cameraList.value = res.items || [];
  } catch (error) {
    console.error('Failed to fetch cameras:', error);
  } finally {
    loading.value = false;
  }
}

async function playCameraInSlot(
  slotIdx: number,
  camera: CameraApi.CameraItem,
  streamType?: 'main' | 'sub',
) {
  const slot = slots.value[slotIdx];
  if (!slot) return;

  const targetStreamType: 'main' | 'sub' =
    streamType || (splitMode.value === 1 ? 'main' : 'sub');

  // 如果原卡槽已有运行中的流且不是同一个摄像头/码流，或者需要重新加载，通知后端释放旧流
  if (slot.camera && slot.streamUrl) {
    try {
      await stopLivePreviewApi(slot.camera.id, slot.streamType);
    } catch (error) {
      console.warn('Failed to stop previous live stream:', error);
    }
  }

  try {
    const res = await startLivePreviewApi(camera.id, targetStreamType);
    slot.camera = camera;
    slot.streamUrl = res.wsUrl || res.httpUrl;
    slot.streamType = targetStreamType;
  } catch (error) {
    console.error('Failed to start preview:', error);
  }
}

/**
 * 获取下一个自动补位或覆盖的目标槽位：
 * 1. 在当前分屏范围内 [0, splitMode) 从 lastAssignedIndex + 1 循环寻找首个空闲槽
 * 2. 如果全满，则循环覆盖下一个槽位：(lastAssignedIndex + 1) % splitMode
 */
function getNextAvailableSlotIndex(): number {
  const max = splitMode.value;
  if (max === 1) return 0;

  const startIdx = Math.max(0, lastAssignedIndex.value);
  for (let i = 1; i <= max; i++) {
    const idx = (startIdx + i) % max;
    if (!slots.value[idx]?.camera) {
      return idx;
    }
  }

  // 全满，循环覆盖下一个槽位
  return (startIdx + 1) % max;
}

function assignCameraToSlot(targetIdx: number, camera: CameraApi.CameraItem) {
  selectedSlotIndex.value = targetIdx;
  lastAssignedIndex.value = targetIdx;
  playCameraInSlot(targetIdx, camera);
}

function handleSelectCamera(camera: CameraApi.CameraItem) {
  const targetSlotIdx = getNextAvailableSlotIndex();
  assignCameraToSlot(targetSlotIdx, camera);
}

function handleSlotClick(idx: number) {
  selectedSlotIndex.value = idx;
}

async function handleCloseSlot(idx: number) {
  const slot = slots.value[idx];
  if (slot) {
    if (slot.camera && slot.streamUrl) {
      try {
        await stopLivePreviewApi(slot.camera.id, slot.streamType);
      } catch (error) {
        console.warn('Failed to stop stream on close:', error);
      }
    }
    slot.camera = null;
    slot.streamUrl = '';
  }
}

function handleSwitchStream(idx: number, newType: 'main' | 'sub') {
  const slot = slots.value[idx];
  if (slot && slot.camera) {
    playCameraInSlot(idx, slot.camera, newType);
  }
}

function setSplitMode(mode: SplitMode) {
  splitMode.value = mode;
  if (selectedSlotIndex.value >= mode) {
    selectedSlotIndex.value = 0;
  }
  if (lastAssignedIndex.value >= mode) {
    lastAssignedIndex.value = -1;
  }
  // 切换分屏时，若从 1 切换为 4/9，自动将已有主码流切换为子码流
  slots.value.forEach((slot, idx) => {
    if (idx < mode && slot.camera && slot.streamUrl) {
      const targetType: 'main' | 'sub' = mode === 1 ? 'main' : 'sub';
      if (slot.streamType !== targetType) {
        playCameraInSlot(idx, slot.camera, targetType);
      }
    }
  });
}

// 拖拽相关处理
function handleCameraDragStart(event: DragEvent, camera: CameraApi.CameraItem) {
  draggingCamera.value = camera;
  draggingSlotIndex.value = null;
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = 'copy';
    event.dataTransfer.setData(
      'text/plain',
      JSON.stringify({ type: 'camera', id: camera.id }),
    );
  }
}

function handleCameraDragEnd() {
  draggingCamera.value = null;
  dragOverSlotIndex.value = null;
}

function handleSlotDragStart(event: DragEvent, slotIdx: number) {
  const slot = slots.value[slotIdx];
  if (!slot?.camera) return;
  draggingSlotIndex.value = slotIdx;
  draggingCamera.value = null;
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData(
      'text/plain',
      JSON.stringify({ type: 'slot', index: slotIdx }),
    );
  }
}

function handleSlotDragEnd() {
  draggingSlotIndex.value = null;
  dragOverSlotIndex.value = null;
}

function handleSlotDragOver(event: DragEvent, targetIdx: number) {
  event.preventDefault();
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect =
      draggingSlotIndex.value === null ? 'copy' : 'move';
  }
  dragOverSlotIndex.value = targetIdx;
}

function handleSlotDragLeave(_event: DragEvent, targetIdx: number) {
  if (dragOverSlotIndex.value === targetIdx) {
    dragOverSlotIndex.value = null;
  }
}

function handleSlotDrop(event: DragEvent, targetIdx: number) {
  event.preventDefault();
  dragOverSlotIndex.value = null;

  // 1. 如果是从左侧列表拖拽摄像头到槽位
  if (draggingCamera.value) {
    const cam = draggingCamera.value;
    draggingCamera.value = null;
    assignCameraToSlot(targetIdx, cam);
    return;
  }

  // 2. 如果是槽位之间的相互拖拽/交换
  if (draggingSlotIndex.value !== null) {
    const sourceIdx = draggingSlotIndex.value;
    draggingSlotIndex.value = null;
    if (sourceIdx === targetIdx) return;

    const sourceSlot = slots.value[sourceIdx];
    const targetSlot = slots.value[targetIdx];
    if (!sourceSlot || !targetSlot) return;

    // 交换槽位内容
    const tempCamera = sourceSlot.camera;
    const tempUrl = sourceSlot.streamUrl;
    const tempType = sourceSlot.streamType;

    sourceSlot.camera = targetSlot.camera;
    sourceSlot.streamUrl = targetSlot.streamUrl;
    sourceSlot.streamType = targetSlot.streamType;

    targetSlot.camera = tempCamera;
    targetSlot.streamUrl = tempUrl;
    targetSlot.streamType = tempType;

    selectedSlotIndex.value = targetIdx;
    return;
  }

  // 3. 兜底 dataTransfer 解析
  try {
    const dataStr = event.dataTransfer?.getData('text/plain');
    if (!dataStr) return;
    const data = JSON.parse(dataStr);
    if (data.type === 'camera' && data.id) {
      const cam = cameraList.value.find((c) => c.id === data.id);
      if (cam) {
        assignCameraToSlot(targetIdx, cam);
      }
    }
  } catch (error) {
    console.error('Failed to handle drop data:', error);
  }
}

onMounted(() => {
  fetchCameras();
});

onBeforeUnmount(() => {
  // 卸载视图时主动释放所有已启动的拉流
  slots.value.forEach((slot) => {
    if (slot.camera && slot.streamUrl) {
      stopLivePreviewApi(slot.camera.id, slot.streamType).catch(() => {});
    }
  });
});
</script>

<template>
  <Page auto-content-height content-class="p-3">
    <div class="h-full flex flex-row overflow-hidden gap-3">
      <!-- 左侧摄像头树/列表 -->
      <div
        class="w-64 bg-card text-card-foreground border border-border rounded-xl flex flex-col p-3 shadow-xs select-none"
      >
        <div class="font-medium text-sm mb-3 flex items-center justify-between">
          <span class="flex items-center space-x-1.5">
            <span>{{ $t('live.selectCamera') }}</span>
          </span>
          <span class="text-xs text-muted-foreground">({{ filteredCameras.length }})</span>
        </div>

        <!-- 搜索框 -->
        <div class="mb-3">
          <div class="relative flex items-center">
            <Search
              class="absolute left-2.5 text-muted-foreground size-3.5 pointer-events-none"
            />
            <input
              v-model="searchKeyword"
              type="text"
              :placeholder="$t('live.searchCamera')"
              class="w-full bg-input-background border border-input rounded text-xs pl-8 pr-2 py-1.5 text-foreground placeholder:text-muted-foreground focus:outline-none focus:border-primary transition-colors"
            />
          </div>
        </div>

        <!-- 列表 -->
        <div class="flex-1 overflow-y-auto space-y-1 pr-1 custom-scrollbar">
          <div
            v-for="camera in filteredCameras"
            :key="camera.id"
            draggable="true"
            class="flex items-center justify-between px-2.5 py-2 rounded text-xs hover:bg-accent/80 hover:text-accent-foreground cursor-pointer transition-colors"
            :class="
              slots[selectedSlotIndex]?.camera?.id === camera.id
                ? 'bg-primary/10 text-primary font-medium'
                : 'text-foreground/80'
            "
            @click="handleSelectCamera(camera)"
            @dragstart="(e) => handleCameraDragStart(e, camera)"
            @dragend="handleCameraDragEnd"
          >
            <div class="flex items-center space-x-2 truncate">
              <span
                class="w-2 h-2 rounded-full inline-block shrink-0"
                :class="
                  camera.lastProbeStatus === 'success'
                    ? 'bg-green-500'
                    : 'bg-muted-foreground/50'
                "
              ></span>
              <span class="truncate">{{ camera.name }}</span>
            </div>
          </div>
          <div
            v-if="filteredCameras.length === 0"
            class="text-center py-6 text-xs text-muted-foreground"
          >
            {{ $t('live.noCameras') }}
          </div>
        </div>
      </div>

      <!-- 右侧分屏预览区域 -->
      <div
        class="flex-1 flex flex-col bg-card border border-border rounded-xl shadow-xs overflow-hidden"
      >
        <!-- 顶部控制条 -->
        <div
          class="h-10 border-b border-border flex items-center justify-between px-3"
        >
          <div class="text-xs font-medium text-muted-foreground">
            {{ $t('live.title') }}
          </div>

          <!-- 分屏控制按钮 -->
          <div
            class="flex items-center space-x-1 bg-muted/60 p-0.5 rounded border border-border"
          >
            <button
              class="px-2 py-1 rounded text-xs flex items-center space-x-1 transition-colors cursor-pointer"
              :class="
                splitMode === 1
                  ? 'bg-primary text-primary-foreground font-medium shadow-xs'
                  : 'text-muted-foreground hover:text-foreground'
              "
              :title="$t('live.split1')"
              @click="setSplitMode(1)"
            >
              <Square class="size-3.5" />
              <span>1</span>
            </button>
            <button
              class="px-2 py-1 rounded text-xs flex items-center space-x-1 transition-colors cursor-pointer"
              :class="
                splitMode === 4
                  ? 'bg-primary text-primary-foreground font-medium shadow-xs'
                  : 'text-muted-foreground hover:text-foreground'
              "
              :title="$t('live.split4')"
              @click="setSplitMode(4)"
            >
              <Grid class="size-3.5" />
              <span>4</span>
            </button>
            <button
              class="px-2 py-1 rounded text-xs flex items-center space-x-1 transition-colors cursor-pointer"
              :class="
                splitMode === 9
                  ? 'bg-primary text-primary-foreground font-medium shadow-xs'
                  : 'text-muted-foreground hover:text-foreground'
              "
              :title="$t('live.split9')"
              @click="setSplitMode(9)"
            >
              <Grid class="size-3.5" />
              <span>9</span>
            </button>
          </div>
        </div>

        <!-- 视频分屏网格 -->
        <div
          class="flex-1 grid gap-1.5 p-1.5 bg-background overflow-hidden"
          :class="gridClass"
        >
          <div
            v-for="slot in visibleSlots"
            :key="slot.index"
            :draggable="!!slot.camera"
            class="relative w-full h-full bg-black rounded overflow-hidden border transition-all"
            :class="[
              selectedSlotIndex === slot.index
                ? 'border-primary ring-1 ring-primary'
                : 'border-border/60',
              dragOverSlotIndex === slot.index
                ? 'border-primary/80 ring-2 ring-primary/80 bg-primary/5'
                : '',
              slot.camera ? 'cursor-grab active:cursor-grabbing' : '',
            ]"
            @click="handleSlotClick(slot.index)"
            @dragstart="(e) => handleSlotDragStart(e, slot.index)"
            @dragend="handleSlotDragEnd"
            @dragover="(e) => handleSlotDragOver(e, slot.index)"
            @dragleave="(e) => handleSlotDragLeave(e, slot.index)"
            @drop="(e) => handleSlotDrop(e, slot.index)"
          >
            <VideoPlayer
              :url="slot.streamUrl"
              :title="slot.camera?.name"
              :stream-type="slot.streamType"
              @close="handleCloseSlot(slot.index)"
              @switch-stream="(t) => handleSwitchStream(slot.index, t)"
            />
          </div>
        </div>
      </div>
    </div>
  </Page>
</template>

<style scoped>
.custom-scrollbar::-webkit-scrollbar {
  width: 4px;
}

.custom-scrollbar::-webkit-scrollbar-thumb {
  background: hsl(var(--muted-foreground) / 30%);
  border-radius: 2px;
}
</style>
