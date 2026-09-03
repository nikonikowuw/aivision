<script setup lang="ts">
import type { CameraApi } from '#/api/core/camera';

import { ref, watch } from 'vue';

import { IconifyIcon } from '@vben/icons';
import { $t } from '@vben/locales';

import { Modal, Spin } from 'ant-design-vue';

import { startLivePreviewApi, stopLivePreviewApi } from '#/api/core/camera';
import VideoPlayer from '#/components/video/VideoPlayer.vue';

interface Props {
  camera?: CameraApi.CameraItem | null;
  open?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  camera: null,
  open: false,
});

const emit = defineEmits<{
  (e: 'update:open', val: boolean): void;
}>();

const streamType = ref<'main' | 'sub'>('main');
const streamUrl = ref('');
const loading = ref(false);
const previewActive = ref(false);

async function startStream(cam: CameraApi.CameraItem, type: 'main' | 'sub') {
  loading.value = true;
  streamUrl.value = '';
  try {
    const res = await startLivePreviewApi(cam.id, type);
    streamUrl.value = res.wsUrl || res.httpUrl;
    previewActive.value = true;
  } catch (error) {
    console.error('Failed to start camera live preview:', error);
  } finally {
    loading.value = false;
  }
}

async function stopStream(cam: CameraApi.CameraItem, type: 'main' | 'sub') {
  if (!previewActive.value) return;
  try {
    await stopLivePreviewApi(cam.id, type);
  } catch (error) {
    console.warn('Failed to stop camera live preview:', error);
  } finally {
    previewActive.value = false;
    streamUrl.value = '';
  }
}

async function handleSwitchStream(newType: 'main' | 'sub') {
  if (streamType.value === newType || !props.camera) return;
  const currentType = streamType.value;
  streamType.value = newType;
  await stopStream(props.camera, currentType);
  await startStream(props.camera, newType);
}

function handleClose() {
  if (props.camera) {
    void stopStream(props.camera, streamType.value);
  }
  emit('update:open', false);
}

watch(
  () => props.open,
  async (isOpen) => {
    if (isOpen && props.camera) {
      streamType.value = 'main';
      await startStream(props.camera, 'main');
    } else if (!isOpen && props.camera) {
      await stopStream(props.camera, streamType.value);
    }
  },
);
</script>

<template>
  <Modal
    :open="open"
    :footer="null"
    :destroy-on-close="true"
    :width="840"
    centered
    wrap-class-name="camera-preview-modal-wrap"
    @cancel="handleClose"
  >
    <template #title>
      <div class="flex items-center justify-between pr-8">
        <div class="flex items-center gap-2.5">
          <div
            class="flex h-7 w-7 items-center justify-center rounded-lg bg-emerald-500/10 text-emerald-500 border border-emerald-500/20"
          >
            <IconifyIcon icon="lucide:video" class="text-sm" />
          </div>
          <div>
            <div class="flex items-center gap-2">
              <span class="text-sm font-semibold text-foreground">
                {{ camera?.name || $t('resource.camera.preview') }}
              </span>
              <span
                v-if="camera?.cameraId"
                class="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground"
              >
                {{ camera.cameraId }}
              </span>
            </div>
          </div>
        </div>

        <!-- 码流切换胶囊 -->
        <div
          v-if="camera"
          class="flex items-center rounded-lg bg-muted/60 p-0.5 border border-border/60 text-xs"
        >
          <button
            type="button"
            class="rounded-md px-2.5 py-1 text-xs font-medium transition-all"
            :class="
              streamType === 'main'
                ? 'bg-card text-emerald-500 shadow-sm font-semibold'
                : 'text-muted-foreground hover:text-foreground'
            "
            @click="handleSwitchStream('main')"
          >
            {{ $t('resource.camera.mainStream') }}
          </button>
          <button
            type="button"
            class="rounded-md px-2.5 py-1 text-xs font-medium transition-all"
            :class="
              streamType === 'sub'
                ? 'bg-card text-emerald-500 shadow-sm font-semibold'
                : 'text-muted-foreground hover:text-foreground'
            "
            @click="handleSwitchStream('sub')"
          >
            {{ $t('resource.camera.subStream') }}
          </button>
        </div>
      </div>
    </template>

    <div class="mt-3">
      <!-- 16:9 播放视窗容器 -->
      <div
        class="relative aspect-video w-full overflow-hidden rounded-xl border border-border/80 bg-black shadow-inner flex items-center justify-center"
      >
        <Spin v-if="loading" size="large" tip="正在建立边缘实时流..." />
        <VideoPlayer
          v-else-if="streamUrl"
          :url="streamUrl"
          :stream-type="streamType"
          :show-controls="true"
          class="h-full w-full"
        />
        <div
          v-else
          class="flex flex-col items-center justify-center gap-2 text-muted-foreground"
        >
          <IconifyIcon
            icon="lucide:video-off"
            class="text-3xl text-muted-foreground/60"
          />
          <span class="text-xs">暂无可用流或连接超时</span>
        </div>
      </div>

      <!-- 底部流信息与自释放提示 -->
      <div
        class="mt-3 flex items-center justify-between rounded-lg bg-muted/40 px-3.5 py-2 text-xs border border-border/50 text-muted-foreground"
      >
        <div class="flex items-center gap-3 font-mono">
          <span class="flex items-center gap-1.5">
            <span
              class="h-1.5 w-1.5 rounded-full bg-emerald-500 animate-pulse"
            ></span>
            <span>实时传输: WebSockets / HTTP-FLV</span>
          </span>
          <span v-if="camera?.lastCodec" class="text-foreground/80">
            编码: {{ camera.lastCodec }}
          </span>
        </div>
        <div
          class="flex items-center gap-1 text-[11px] text-muted-foreground/80"
        >
          <IconifyIcon icon="lucide:shield-check" class="text-emerald-500" />
          <span>关闭视窗将立即释放边缘解码与拉流资源</span>
        </div>
      </div>
    </div>
  </Modal>
</template>
