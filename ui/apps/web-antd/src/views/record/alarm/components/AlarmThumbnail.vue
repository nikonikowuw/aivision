<script lang="ts" setup>
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue';

import { useAccessStore } from '@vben/stores';

import { Image as AntImage } from 'ant-design-vue';

import { getAlarmImageBlobApi } from '#/api/core/alarm';

const props = defineProps<{
  bbox?: [number, number, number, number]; // [x1, y1, x2, y2] 归一化坐标
  height?: number;
  imageId?: string;
  width?: number;
}>();

const accessStore = useAccessStore();
const containerRef = ref<HTMLDivElement | null>(null);
const imgRef = ref<HTMLImageElement | null>(null);

const hdPreviewDataUrl = ref<string>('');
const isError = ref<boolean>(false);
const previewVisible = ref<boolean>(false);
const previewImageSrc = ref<string>('');

const overlayStyle = ref<Record<string, string>>({
  left: '0px',
  top: '0px',
  width: '100%',
  height: '100%',
});

// 使用原生全景缩略图直链（带 Token 鉴权，由浏览器 C++ 内核多线程流式懒加载）
const thumbnailUrl = computed(() => {
  if (!props.imageId) return '';
  const token = accessStore.accessToken;
  const baseUrl = `/api/record/images/${encodeURIComponent(props.imageId)}?type=thumb`;
  return token ? `${baseUrl}&token=${encodeURIComponent(token)}` : baseUrl;
});

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
  const lenX = Math.min(bw * 0.2, 8);
  const lenY = Math.min(bh * 0.2, 8);

  return [
    `M ${bx} ${by + lenY} L ${bx} ${by} L ${bx + lenX} ${by}`,
    `M ${bx + bw - lenX} ${by} L ${bx + bw} ${by} L ${bx + bw} ${by + lenY}`,
    `M ${bx} ${by + bh - lenY} L ${bx} ${by + bh} L ${bx + lenX} ${by + bh}`,
    `M ${bx + bw - lenX} ${by + bh} L ${bx + bw} ${by + bh} L ${bx + bw} ${by + bh - lenY}`,
  ].join(' ');
});

function updateOverlayGeometry() {
  if (!imgRef.value || !containerRef.value) return;
  const img = imgRef.value;
  const container = containerRef.value;
  const nw = img.naturalWidth;
  const nh = img.naturalHeight;
  if (!nw || !nh) return;

  const cw = container.clientWidth;
  const ch = container.clientHeight;
  if (!cw || !ch) return;

  // 默认 object-contain 模式下的等比定位计算
  const imgRatio = nw / nh;
  const containerRatio = cw / ch;
  if (imgRatio > containerRatio) {
    // 黑边在上下
    const renderW = cw;
    const renderH = cw / imgRatio;
    const renderTop = (ch - renderH) / 2;
    overlayStyle.value = {
      left: '0px',
      top: `${renderTop}px`,
      width: `${renderW}px`,
      height: `${renderH}px`,
    };
  } else {
    // 黑边在左右
    const renderH = ch;
    const renderW = ch * imgRatio;
    const renderLeft = (cw - renderW) / 2;
    overlayStyle.value = {
      left: `${renderLeft}px`,
      top: '0px',
      width: `${renderW}px`,
      height: `${renderH}px`,
    };
  }
}

function onImageLoad() {
  isError.value = false;
  nextTick(() => {
    updateOverlayGeometry();
  });
}

let resizeObserver: null | ResizeObserver = null;

onMounted(() => {
  if (containerRef.value && typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(() => {
      updateOverlayGeometry();
    });
    resizeObserver.observe(containerRef.value);
  }
});

onUnmounted(() => {
  if (resizeObserver) {
    resizeObserver.disconnect();
    resizeObserver = null;
  }
});

watch(
  () => [props.imageId, props.width, props.height],
  () => {
    isError.value = false;
    hdPreviewDataUrl.value = '';
    previewImageSrc.value = '';
    nextTick(() => {
      updateOverlayGeometry();
    });
  },
);

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
      hdImg.addEventListener('load', resolve, { once: true });
      hdImg.addEventListener('error', reject, { once: true });
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
      offscreenCtx.shadowColor = 'rgba(0, 0, 0, 0.7)';
      offscreenCtx.shadowBlur = 4;
      offscreenCtx.lineWidth = Math.max(2, Math.round(hdW / 700));
      offscreenCtx.strokeStyle = '#ef4444';
      offscreenCtx.fillStyle = 'rgba(239, 68, 68, 0.12)';
      offscreenCtx.fillRect(minX, minY, boxW, boxH);
      offscreenCtx.strokeRect(minX, minY, boxW, boxH);

      // 四角同色系高亮包角
      const cornerLen = Math.min(boxW, boxH) * 0.18;
      offscreenCtx.lineWidth = Math.max(3, Math.round(hdW / 450));
      offscreenCtx.strokeStyle = '#f87171';
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
  } catch (error) {
    console.error('Failed to render HD panorama preview:', error);
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
    ref="containerRef"
    class="relative flex mx-auto cursor-pointer items-center justify-center overflow-hidden rounded border border-border bg-muted/40 transition hover:opacity-90"
    :style="containerStyle"
    @click="handlePreviewClick"
  >
    <span
      v-if="!thumbnailUrl || isError"
      class="text-[10px] text-muted-foreground"
    >
      无图
    </span>
    <!-- 列表展示图：直接使用原生 <img> + loading="lazy"，由浏览器底层 C++ 线程并行解码与渲染 -->
    <template v-else>
      <img
        ref="imgRef"
        :src="thumbnailUrl"
        loading="lazy"
        class="h-full w-full object-contain"
        alt="alarm panorama"
        @load="onImageLoad"
        @error="() => (isError = true)"
      />

      <!-- 紧贴图片实际渲染区域的 SVG 目标红框指示 (纯矢量层覆盖，0 坐标偏移) -->
      <svg
        v-if="normalizedBbox"
        viewBox="0 0 100 100"
        preserveAspectRatio="none"
        class="pointer-events-none absolute"
        :style="overlayStyle"
      >
        <rect
          :x="normalizedBbox[0] * 100"
          :y="normalizedBbox[1] * 100"
          :width="(normalizedBbox[2] - normalizedBbox[0]) * 100"
          :height="(normalizedBbox[3] - normalizedBbox[1]) * 100"
          fill="rgba(239, 68, 68, 0.12)"
          stroke="#ef4444"
          stroke-width="1.5"
          vector-effect="non-scaling-stroke"
          style="filter: drop-shadow(0 0 1.5px rgb(0 0 0 / 70%))"
        />
        <path
          :d="cornerBracketPath"
          fill="none"
          stroke="#f87171"
          stroke-width="2.5"
          stroke-linecap="square"
          vector-effect="non-scaling-stroke"
        />
      </svg>
    </template>

    <!-- 点击放大预览大图 -->
    <div v-if="thumbnailUrl && !isError" style="display: none">
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
