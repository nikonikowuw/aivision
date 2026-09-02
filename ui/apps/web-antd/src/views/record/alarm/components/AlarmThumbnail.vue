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

const containerStyle = computed(() => {
  const style: Record<string, string> = {};
  if (typeof props.width === 'number' && props.width > 0) {
    style.width = `${props.width}px`;
  }
  if (typeof props.height === 'number' && props.height > 0) {
    style.height = `${props.height}px`;
  }
  return style;
});

const normalizedBbox = computed<[number, number, number, number] | null>(() => {
  if (!props.bbox || !Array.isArray(props.bbox) || props.bbox.length < 4) {
    return null;
  }
  const [x1, y1, x2, y2] = props.bbox;
  if (
    !Number.isFinite(x1) ||
    !Number.isFinite(y1) ||
    !Number.isFinite(x2) ||
    !Number.isFinite(y2)
  ) {
    return null;
  }
  const minX = Math.min(x1, x2);
  const minY = Math.min(y1, y2);
  const maxX = Math.max(x1, x2);
  const maxY = Math.max(y1, y2);
  if (maxX <= minX || maxY <= minY) return null;
  return [minX, minY, maxX, maxY];
});

const cornerBracketPath = computed(() => {
  if (!normalizedBbox.value) return '';
  const [minX, minY, maxX, maxY] = normalizedBbox.value;
  const bx = minX * 100;
  const by = minY * 100;
  const bw = (maxX - minX) * 100;
  const bh = (maxY - minY) * 100;
  const lenX = Math.min(bw * 0.22, 10);
  const lenY = Math.min(bh * 0.22, 10);

  return [
    `M ${bx} ${by + lenY} L ${bx} ${by} L ${bx + lenX} ${by}`,
    `M ${bx + bw - lenX} ${by} L ${bx + bw} ${by} L ${bx + bw} ${by + lenY}`,
    `M ${bx} ${by + bh - lenY} L ${bx} ${by + bh} L ${bx + lenX} ${by + bh}`,
    `M ${bx + bw - lenX} ${by + bh} L ${bx + bw} ${by + bh} L ${bx + bw} ${by + bh - lenY}`,
  ].join(' ');
});

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
      offscreenCtx.lineWidth = Math.max(2, Math.round(hdW / 600));
      offscreenCtx.strokeStyle = '#ef4444';
      offscreenCtx.fillStyle = 'rgba(239, 68, 68, 0.18)';
      offscreenCtx.fillRect(minX, minY, boxW, boxH);
      offscreenCtx.strokeRect(minX, minY, boxW, boxH);

      // 四角直角包角
      const cornerLen = Math.min(boxW, boxH) * 0.22;
      offscreenCtx.lineWidth = Math.max(3, Math.round(hdW / 400));
      offscreenCtx.strokeStyle = '#ef4444';
      offscreenCtx.lineCap = 'square';

      // 左上
      offscreenCtx.beginPath();
      offscreenCtx.moveTo(minX, minY + cornerLen);
      offscreenCtx.lineTo(minX, minY);
      offscreenCtx.lineTo(minX + cornerLen, minY);
      offscreenCtx.stroke();

      // 右上
      offscreenCtx.beginPath();
      offscreenCtx.moveTo(minX + boxW - cornerLen, minY);
      offscreenCtx.lineTo(minX + boxW, minY);
      offscreenCtx.lineTo(minX + boxW, minY + cornerLen);
      offscreenCtx.stroke();

      // 左下
      offscreenCtx.beginPath();
      offscreenCtx.moveTo(minX, minY + boxH - cornerLen);
      offscreenCtx.lineTo(minX, minY + boxH);
      offscreenCtx.lineTo(minX + cornerLen, minY + boxH);
      offscreenCtx.stroke();

      // 右下
      offscreenCtx.beginPath();
      offscreenCtx.moveTo(minX + boxW - cornerLen, minY + boxH);
      offscreenCtx.lineTo(minX + boxW, minY + boxH);
      offscreenCtx.lineTo(minX + boxW, minY + boxH - cornerLen);
      offscreenCtx.stroke();

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
    class="relative flex mx-auto cursor-pointer items-center justify-center overflow-hidden rounded border border-border bg-muted/40 transition hover:opacity-90"
    :style="containerStyle"
    @click="handlePreviewClick"
  >
    <span v-if="!thumbnailUrl || isError" class="text-[10px] text-muted-foreground">
      无图
    </span>
    <!-- 列表展示图：直接使用原生 <img> + loading="lazy"，由浏览器底层 C++ 线程并行解码与渲染 -->
    <template v-else>
      <img
        :src="thumbnailUrl"
        loading="lazy"
        class="h-full w-full object-contain"
        alt="alarm panorama"
        @error="() => (isError = true)"
      />

      <!-- 缩略图上的矢量目标红框指示 (纯 SVG 矢量层覆盖，0 JS CPU 消耗) -->
      <svg
        v-if="normalizedBbox"
        viewBox="0 0 100 100"
        preserveAspectRatio="none"
        class="pointer-events-none absolute inset-0 h-full w-full"
      >
        <rect
          :x="normalizedBbox[0] * 100"
          :y="normalizedBbox[1] * 100"
          :width="(normalizedBbox[2] - normalizedBbox[0]) * 100"
          :height="(normalizedBbox[3] - normalizedBbox[1]) * 100"
          fill="rgba(239, 68, 68, 0.18)"
          stroke="#ef4444"
          stroke-width="1.5"
          vector-effect="non-scaling-stroke"
        />
        <path
          :d="cornerBracketPath"
          fill="none"
          stroke="#ef4444"
          stroke-width="2.5"
          stroke-linecap="square"
          vector-effect="non-scaling-stroke"
        />
      </svg>
    </template>

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

