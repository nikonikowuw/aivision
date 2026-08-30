<script lang="ts" setup>
import { computed, ref, watch } from 'vue';

import { useAccessStore } from '@vben/stores';

import { Image as AntImage } from 'ant-design-vue';

import { getAlarmImageBlobApi } from '#/api/core/alarm';

const props = defineProps<{
  imageId?: string;
  bbox?: [number, number, number, number]; // [x1, y1, x2, y2] 归一化坐标
  width?: number;
  height?: number;
}>();

const accessStore = useAccessStore();
const hdPreviewDataUrl = ref<string>('');

// 使用原生全景缩略图直链（带 Token 鉴权，由浏览器 C++ 内核多线程流式懒加载）
const thumbnailUrl = computed(() => {
  if (!props.imageId) return '';
  const token = accessStore.accessToken;
  const baseUrl = `/api/record/images/${encodeURIComponent(props.imageId)}?type=thumb`;
  return token ? `${baseUrl}&token=${encodeURIComponent(token)}` : baseUrl;
});

watch(
  () => props.imageId,
  () => {
    isError.value = false;
    hdPreviewDataUrl.value = '';
    previewImageSrc.value = '';
  },
);

const isError = ref<boolean>(false);
const previewVisible = ref<boolean>(false);
const previewImageSrc = ref<string>('');

/**
 * 用户点击列表缩略图放大查看全景时，按需从 1080P/4K 高清原图无损生成带红框全景超清图
 */
async function getHdPanoramaPreview(): Promise<string> {
  if (hdPreviewDataUrl.value) return hdPreviewDataUrl.value;
  if (!props.imageId) return thumbnailUrl.value;

  try {
    const fullBlob = await getAlarmImageBlobApi(props.imageId, false);
    if (!fullBlob) return thumbnailUrl.value;

    const fullBlobUrl = URL.createObjectURL(fullBlob);
    const hdImg = new Image();
    await new Promise((resolve, reject) => {
      hdImg.onload = resolve;
      hdImg.onerror = reject;
      hdImg.src = fullBlobUrl;
    });

    const hdW = hdImg.naturalWidth || hdImg.width;
    const hdH = hdImg.naturalHeight || hdImg.height;
    if (!hdW || !hdH) {
      URL.revokeObjectURL(fullBlobUrl);
      return thumbnailUrl.value;
    }

    const offscreenCanvas = document.createElement('canvas');
    offscreenCanvas.width = hdW;
    offscreenCanvas.height = hdH;
    const offscreenCtx = offscreenCanvas.getContext('2d');
    if (!offscreenCtx) {
      URL.revokeObjectURL(fullBlobUrl);
      return thumbnailUrl.value;
    }

    offscreenCtx.drawImage(hdImg, 0, 0, hdW, hdH);

    if (props.bbox && props.bbox.length === 4) {
      const [x1, y1, x2, y2] = props.bbox;
      const minX = Math.min(x1, x2) * hdW;
      const minY = Math.min(y1, y2) * hdH;
      const boxW = Math.abs(x2 - x1) * hdW;
      const boxH = Math.abs(y2 - y1) * hdH;

      offscreenCtx.save();
      offscreenCtx.lineWidth = Math.max(3, Math.round(hdW / 300));
      offscreenCtx.strokeStyle = '#ef4444';
      offscreenCtx.fillStyle = 'rgba(239, 68, 68, 0.15)';
      offscreenCtx.fillRect(minX, minY, boxW, boxH);
      offscreenCtx.strokeRect(minX, minY, boxW, boxH);
      offscreenCtx.restore();
    }

    hdPreviewDataUrl.value = offscreenCanvas.toDataURL('image/jpeg', 0.95);
    URL.revokeObjectURL(fullBlobUrl);
    return hdPreviewDataUrl.value;
  } catch (e) {
    console.error('Failed to render HD panorama preview:', e);
    return thumbnailUrl.value;
  }
}

async function handlePreviewClick() {
  previewVisible.value = true;
  previewImageSrc.value = await getHdPanoramaPreview();
}
</script>

<template>
  <div
    :key="props.imageId"
    class="relative flex cursor-pointer items-center justify-center overflow-hidden rounded border border-border bg-muted/40 transition hover:opacity-90"
    :style="{
      width: (props.width || 64) + 'px',
      height: (props.height || 48) + 'px',
    }"
    @click="handlePreviewClick"
  >
    <span v-if="!thumbnailUrl || isError" class="text-[10px] text-muted-foreground">
      无图
    </span>
    <!-- 列表展示图：直接使用原生 <img> + loading="lazy"，由浏览器底层 C++ 线程并行解码与渲染 -->
    <img
      v-else
      :src="thumbnailUrl"
      :width="props.width || 64"
      :height="props.height || 48"
      loading="lazy"
      class="h-full w-full object-cover"
      alt="alarm panorama"
      @error="() => (isError = true)"
    />

    <!-- 点击放大预览弹窗（无损从高清原图 1080P/4K 生成） -->
    <div style="display: none">
      <AntImage
        :preview="{
          visible: previewVisible,
          src: previewImageSrc || thumbnailUrl,
          onVisibleChange: (val: boolean) => (previewVisible = val),
        }"
      />
    </div>
  </div>
</template>

