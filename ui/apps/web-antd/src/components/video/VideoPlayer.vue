<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';

import { Camera, Expand, LoaderCircle, RotateCw, X } from '@vben/icons';
import { $t } from '@vben/locales';

import mpegts from 'mpegts.js';

interface Props {
  url?: string;
  title?: string;
  autoplay?: boolean;
  isLive?: boolean;
  streamType?: 'main' | 'sub';
  showControls?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  url: '',
  title: '',
  autoplay: true,
  isLive: true,
  streamType: 'main',
  showControls: true,
});

const emit = defineEmits<{
  (e: 'close'): void;
  (e: 'metadata', size: { height: number; width: number }): void;
  (e: 'switchStream', type: 'main' | 'sub'): void;
  (e: 'error', err: any): void;
}>();

const videoRef = ref<HTMLVideoElement | null>(null);
const containerRef = ref<HTMLDivElement | null>(null);
const loading = ref(false);
const error = ref<null | string>(null);
const isFullscreen = ref(false);

const videoResolution = ref('');
const videoFps = ref<null | number>(null);
const decoderType = ref('MSE (Hardware)');

let player: mpegts.Player | null = null;
let statsTimer: null | number = null;
let reconnectTimer: null | number = null;
let retryCount = 0;
const MAX_RETRY_COUNT = 3;

function emitVideoMetadata() {
  const video = videoRef.value;
  if (!video || video.videoWidth <= 0 || video.videoHeight <= 0) return;
  emit('metadata', { height: video.videoHeight, width: video.videoWidth });
}

function handleVideoPlaying() {
  loading.value = false;
  error.value = null;
  emitVideoMetadata();
  if (
    videoRef.value &&
    videoRef.value.videoWidth &&
    videoRef.value.videoHeight
  ) {
    videoResolution.value = `${videoRef.value.videoWidth}x${videoRef.value.videoHeight}`;
  }
}

function handleVideoWaiting() {
  loading.value = true;
}

function removeVideoEventListeners() {
  videoRef.value?.removeEventListener('loadedmetadata', emitVideoMetadata);
  videoRef.value?.removeEventListener('resize', emitVideoMetadata);
  videoRef.value?.removeEventListener('playing', handleVideoPlaying);
  videoRef.value?.removeEventListener('waiting', handleVideoWaiting);
}

function destroyPlayer() {
  removeVideoEventListeners();
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
  if (statsTimer) {
    clearInterval(statsTimer);
    statsTimer = null;
  }
  if (player) {
    try {
      player.pause();
      player.unload();
      player.detachMediaElement();
      player.destroy();
    } catch (error) {
      console.warn('Error destroying mpegts player:', error);
    }
    player = null;
  }
  videoResolution.value = '';
  videoFps.value = null;
}

function initPlayer(isRetry = false) {
  destroyPlayer();
  error.value = null;

  if (!isRetry) {
    retryCount = 0;
  }

  if (!props.url || !videoRef.value) {
    return;
  }

  if (!mpegts.isSupported()) {
    error.value = 'MPEG-TS is not supported in this browser';
    emit('error', new Error(error.value));
    return;
  }

  loading.value = true;

  try {
    player = mpegts.createPlayer(
      {
        type: 'flv',
        isLive: props.isLive,
        url: props.url,
        hasAudio: false,
        cors: true,
      },
      {
        enableWorker: true,
        enableStashBuffer: false,
        stashInitialSize: 128,
        liveBufferLatencyChasing: true,
        liveBufferLatencyMaxLatency: 2.5,
        liveBufferLatencyMinRemain: 0.5,
        autoCleanupSourceBuffer: true,
        autoCleanupMaxBackwardDuration: 10,
        autoCleanupMinBackwardDuration: 5,
      },
    );

    player.attachMediaElement(videoRef.value);
    player.load();

    if (props.autoplay) {
      const playPromise = player.play();
      if (playPromise && typeof playPromise.catch === 'function') {
        playPromise.catch((error: unknown) => {
          console.warn('Autoplay error:', error);
        });
      }
    }

    player.on(mpegts.Events.ERROR, (errType, errDetail, errInfo) => {
      console.warn('Player error:', errType, errDetail, errInfo);
      // 网络建连早期若因首帧或断连触发 NetworkError，且重试次数内，进行平滑延迟重连
      if (
        (errType === mpegts.ErrorTypes.NETWORK_ERROR ||
          errDetail === 'Exception') &&
        retryCount < MAX_RETRY_COUNT &&
        props.url
      ) {
        retryCount += 1;
        loading.value = true;
        reconnectTimer = window.setTimeout(() => {
          if (props.url) {
            initPlayer(true);
          }
        }, 1000);
        return;
      }

      loading.value = false;
      error.value = `${errType}: ${errDetail}`;
      emit('error', { type: errType, detail: errDetail, info: errInfo });
    });

    player.on(mpegts.Events.LOADING_COMPLETE, () => {
      loading.value = false;
    });

    player.on(mpegts.Events.STATISTICS_INFO, (statInfo: any) => {
      if (statInfo.currentFPS) {
        videoFps.value = Math.round(statInfo.currentFPS);
      }
    });

    player.on(mpegts.Events.MEDIA_INFO, (mediaInfo: any) => {
      if (mediaInfo.width && mediaInfo.height) {
        videoResolution.value = `${mediaInfo.width}x${mediaInfo.height}`;
        emit('metadata', {
          height: Number(mediaInfo.height),
          width: Number(mediaInfo.width),
        });
      }
      if (mediaInfo.fps) {
        videoFps.value = Math.round(mediaInfo.fps);
      }
    });

    // 监听原生视频事件，向叠加层提供真实视频宽高并同步 loading 状态。
    videoRef.value.addEventListener('loadedmetadata', emitVideoMetadata);
    videoRef.value.addEventListener('resize', emitVideoMetadata);
    videoRef.value.addEventListener('playing', handleVideoPlaying);
    videoRef.value.addEventListener('waiting', handleVideoWaiting);
  } catch (error: any) {
    loading.value = false;
    error.value = error?.message || 'Player initialization failed';
    emit('error', error);
  }
}

function handleRefresh() {
  initPlayer();
}

function handleScreenshot() {
  if (!videoRef.value) return;
  try {
    const video = videoRef.value;
    const canvas = document.createElement('canvas');
    canvas.width = video.videoWidth || video.clientWidth || 1280;
    canvas.height = video.videoHeight || video.clientHeight || 720;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    ctx.drawImage(video, 0, 0, canvas.width, canvas.height);
    const dataURL = canvas.toDataURL('image/png');
    const link = document.createElement('a');
    link.href = dataURL;
    link.download = `${props.title || 'camera'}_${Date.now()}.png`;
    document.body.append(link);
    link.click();
    link.remove();
  } catch (error) {
    console.error('Screenshot failed:', error);
  }
}

function toggleFullscreen() {
  if (!containerRef.value) return;
  if (document.fullscreenElement) {
    document.exitFullscreen?.().then(() => {
      isFullscreen.value = false;
    });
  } else {
    containerRef.value.requestFullscreen?.().then(() => {
      isFullscreen.value = true;
    });
  }
}

function onFullscreenChange() {
  isFullscreen.value = !!document.fullscreenElement;
}

watch(
  () => props.url,
  (newUrl) => {
    if (newUrl) {
      initPlayer();
    } else {
      destroyPlayer();
    }
  },
);

onMounted(() => {
  document.addEventListener('fullscreenchange', onFullscreenChange);
  if (props.url) {
    initPlayer();
  }
});

onBeforeUnmount(() => {
  document.removeEventListener('fullscreenchange', onFullscreenChange);
  destroyPlayer();
});
</script>

<template>
  <div
    ref="containerRef"
    class="video-player-container relative w-full h-full bg-black flex items-center justify-center overflow-hidden group select-none"
  >
    <video
      ref="videoRef"
      class="w-full h-full object-contain pointer-events-none"
      muted
      playsinline
    ></video>

    <!-- 顶部状态栏 -->
    <div
      v-if="showControls && (title || streamType)"
      class="absolute top-0 left-0 right-0 p-2 bg-gradient-to-b from-black/70 to-transparent flex items-center justify-between text-white text-xs opacity-0 group-hover:opacity-100 transition-opacity z-10"
    >
      <div class="flex items-center space-x-2 truncate">
        <span class="font-medium truncate">{{
          title || $t('live.noSignal')
        }}</span>
        <span
          v-if="url"
          class="px-1.5 py-0.5 rounded text-[10px]"
          :class="
            streamType === 'main'
              ? 'bg-blue-600/80 text-white'
              : 'bg-green-600/80 text-white'
          "
        >
          {{
            streamType === 'main' ? $t('live.mainStream') : $t('live.subStream')
          }}
        </span>
      </div>
      <div class="flex items-center space-x-2">
        <button
          v-if="url"
          class="hover:text-primary transition-colors cursor-pointer"
          :title="$t('live.closeStream')"
          @click="emit('close')"
        >
          <X class="size-3.5" />
        </button>
      </div>
    </div>

    <!-- 无信号 / 错误占位 -->
    <div
      v-if="!url || error"
      class="absolute inset-0 flex flex-col items-center justify-center text-zinc-500 space-y-2 p-4 text-center z-0"
    >
      <span class="text-xs">{{ error ? error : $t('live.noSignal') }}</span>
      <button
        v-if="error"
        class="mt-2 text-xs text-blue-400 hover:text-blue-300 flex items-center space-x-1 cursor-pointer"
        @click="handleRefresh"
      >
        <RotateCw class="size-3.5" />
        <span>{{ $t('live.refresh') }}</span>
      </button>
    </div>

    <!-- 加载中 -->
    <div
      v-if="loading && url && !error"
      class="absolute inset-0 flex flex-col items-center justify-center bg-black/40 text-white space-y-2 z-5"
    >
      <LoaderCircle class="size-6 animate-spin" />
      <span class="text-xs">{{ $t('live.buffering') }}</span>
    </div>

    <!-- 底部控制悬浮条 -->
    <div
      v-if="showControls && url && !error"
      class="absolute bottom-0 left-0 right-0 p-2 bg-gradient-to-t from-black/70 to-transparent flex items-center justify-between text-white text-xs opacity-0 group-hover:opacity-100 transition-opacity z-10"
    >
      <div class="flex items-center space-x-2">
        <button
          class="hover:text-primary transition-colors cursor-pointer"
          :title="$t('live.refresh')"
          @click="handleRefresh"
        >
          <RotateCw class="size-3.5" />
        </button>
        <button
          class="hover:text-primary transition-colors cursor-pointer"
          :title="$t('live.screenshot')"
          @click="handleScreenshot"
        >
          <Camera class="size-3.5" />
        </button>
        <button
          class="text-[11px] px-1.5 py-0.5 rounded bg-zinc-800/80 hover:bg-zinc-700 cursor-pointer"
          @click="emit('switchStream', streamType === 'main' ? 'sub' : 'main')"
        >
          {{
            streamType === 'main' ? $t('live.subStream') : $t('live.mainStream')
          }}
        </button>
      </div>

      <!-- 右侧诊断信息与全屏控制 -->
      <div class="flex items-center space-x-2 text-[10px] text-zinc-400">
        <span v-if="videoResolution">{{ videoResolution }}</span>
        <span v-if="videoFps">{{ videoFps }}fps</span>
        <span class="hidden md:inline px-1 py-0.5 rounded bg-zinc-800/60">{{
          decoderType
        }}</span>
        <button
          class="hover:text-primary transition-colors cursor-pointer text-white"
          :title="
            isFullscreen ? $t('live.exitFullScreen') : $t('live.fullScreen')
          "
          @click="toggleFullscreen"
        >
          <Expand class="size-3.5" />
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.video-player-container {
  background-color: #0c0d0e;
}
</style>
