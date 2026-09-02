<script lang="ts" setup>
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue';

import { $t } from '@vben/locales';
import { useAccessStore } from '@vben/stores';

import { Image as AntImage, Spin } from 'ant-design-vue';

const props = withDefaults(
  defineProps<{
    alt?: string;
    bbox?: [number, number, number, number] | number[];
    fit?: 'contain' | 'cover' | 'fill';
    height?: number;
    original?: boolean; // 是否直接使用原图（例如详情卡片中展示高清原图）
    preview?: boolean; // 是否允许点击放大预览
    url?: string;
    width?: number;
  }>(),
  {
    alt: '',
    bbox: undefined,
    fit: 'cover',
    height: undefined,
    original: false,
    preview: true,
    url: '',
    width: undefined,
  },
);

const accessStore = useAccessStore();
const containerRef = ref<HTMLDivElement | null>(null);
const imgRef = ref<HTMLImageElement | null>(null);

const isError = ref<boolean>(false);
const previewVisible = ref<boolean>(false);
const previewImageSrc = ref<string>('');
const previewLoading = ref<boolean>(false);
const hdPreviewDataUrl = ref<string>('');

const overlayStyle = ref<Record<string, string>>({
  left: '0px',
  top: '0px',
  width: '100%',
  height: '100%',
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

// 原生缩略图直链（带 Token 鉴权，默认 type=thumb 由浏览器 C++ 内核多线程流式懒加载）
const thumbnailUrl = computed(() => {
  if (!props.url) return '';
  const token = accessStore.accessToken;
  let base = props.url;
  if (!props.original) {
    const delimiter = base.includes('?') ? '&' : '?';
    base = `${base}${delimiter}type=thumb`;
  }
  const tokenDelimiter = base.includes('?') ? '&' : '?';
  return token
    ? `${base}${tokenDelimiter}token=${encodeURIComponent(token)}`
    : base;
});

// 高清原图直链（用于按需大图预览与 Canvas 无损绘制）
const authOriginalUrl = computed(() => {
  if (!props.url) return '';
  const token = accessStore.accessToken;
  const delimiter = props.url.includes('?') ? '&' : '?';
  return token
    ? `${props.url}${delimiter}token=${encodeURIComponent(token)}`
    : props.url;
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

  if (props.fit === 'contain') {
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
  } else if (props.fit === 'cover') {
    const imgRatio = nw / nh;
    const containerRatio = cw / ch;
    if (imgRatio > containerRatio) {
      // 左右被裁切
      const scale = ch / nh;
      const scaledW = nw * scale;
      const offsetX = (cw - scaledW) / 2;
      overlayStyle.value = {
        left: `${offsetX}px`,
        top: '0px',
        width: `${scaledW}px`,
        height: `${ch}px`,
      };
    } else {
      // 上下被裁切
      const scale = cw / nw;
      const scaledH = nh * scale;
      const offsetY = (ch - scaledH) / 2;
      overlayStyle.value = {
        left: '0px',
        top: `${offsetY}px`,
        width: `${cw}px`,
        height: `${scaledH}px`,
      };
    }
  } else {
    overlayStyle.value = {
      left: '0px',
      top: '0px',
      width: '100%',
      height: '100%',
    };
  }
}

function onImageLoad() {
  isError.value = false;
  nextTick(() => {
    updateOverlayGeometry();
  });
}

let resizeObserver: ResizeObserver | null = null;

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
  () => [props.url, props.fit, props.width, props.height],
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
 * 仅当用户主动点击图片查看大图预览时，按需从 1080P/4K 高清原图无损生成带目标框全景图
 */
async function getHdPanoramaPreview(): Promise<string> {
  if (hdPreviewDataUrl.value) return hdPreviewDataUrl.value;
  if (!authOriginalUrl.value) return thumbnailUrl.value;

  // 如果没有目标框，直接返回高清原图直链
  if (!normalizedBbox.value) {
    return authOriginalUrl.value;
  }

  let fullBlobUrl = '';
  try {
    const resp = await fetch(authOriginalUrl.value, {
      headers: {
        Authorization: `Bearer ${accessStore.accessToken || ''}`,
      },
    });
    if (!resp.ok) return authOriginalUrl.value;

    const blob = await resp.blob();
    fullBlobUrl = URL.createObjectURL(blob);

    const hdImg = new Image();
    await new Promise((resolve, reject) => {
      hdImg.addEventListener('load', resolve, { once: true });
      hdImg.addEventListener('error', reject, { once: true });
      hdImg.src = fullBlobUrl;
    });

    const hdW = hdImg.naturalWidth || hdImg.width;
    const hdH = hdImg.naturalHeight || hdImg.height;
    if (!hdW || !hdH || !normalizedBbox.value) {
      return authOriginalUrl.value;
    }

    const offscreenCanvas = document.createElement('canvas');
    offscreenCanvas.width = hdW;
    offscreenCanvas.height = hdH;
    const offscreenCtx = offscreenCanvas.getContext('2d');
    if (!offscreenCtx) {
      URL.revokeObjectURL(fullBlobUrl);
      return authOriginalUrl.value;
    }

    offscreenCtx.drawImage(hdImg, 0, 0, hdW, hdH);

    const [minX, minY, maxX, maxY] = normalizedBbox.value;
    const px = minX * hdW;
    const py = minY * hdH;
    const pw = (maxX - minX) * hdW;
    const ph = (maxY - minY) * hdH;

    offscreenCtx.save();
    offscreenCtx.shadowColor = 'rgba(0, 0, 0, 0.65)';
    offscreenCtx.shadowBlur = 4;
    offscreenCtx.lineWidth = Math.max(2, Math.round(hdW / 700));
    offscreenCtx.strokeStyle = '#0ea5e9';
    offscreenCtx.fillStyle = 'rgba(14, 165, 233, 0.12)';
    offscreenCtx.fillRect(px, py, pw, ph);
    offscreenCtx.strokeRect(px, py, pw, ph);

    // 绘制精致四角同色系包角
    const cornerLen = Math.min(pw, ph) * 0.18;
    offscreenCtx.lineWidth = Math.max(3, Math.round(hdW / 450));
    offscreenCtx.strokeStyle = '#38bdf8';
    offscreenCtx.lineCap = 'square';

    // 左上
    offscreenCtx.beginPath();
    offscreenCtx.moveTo(px, py + cornerLen);
    offscreenCtx.lineTo(px, py);
    offscreenCtx.lineTo(px + cornerLen, py);
    offscreenCtx.stroke();

    // 右上
    offscreenCtx.beginPath();
    offscreenCtx.moveTo(px + pw - cornerLen, py);
    offscreenCtx.lineTo(px + pw, py);
    offscreenCtx.lineTo(px + pw, py + cornerLen);
    offscreenCtx.stroke();

    // 左下
    offscreenCtx.beginPath();
    offscreenCtx.moveTo(px, py + ph - cornerLen);
    offscreenCtx.lineTo(px, py + ph);
    offscreenCtx.lineTo(px + cornerLen, py + ph);
    offscreenCtx.stroke();

    // 右下
    offscreenCtx.beginPath();
    offscreenCtx.moveTo(px + pw - cornerLen, py + ph);
    offscreenCtx.lineTo(px + pw, py + ph);
    offscreenCtx.lineTo(px + pw, py + ph - cornerLen);
    offscreenCtx.stroke();

    offscreenCtx.restore();

    hdPreviewDataUrl.value = offscreenCanvas.toDataURL('image/jpeg', 0.95);
    return hdPreviewDataUrl.value;
  } catch (error) {
    console.error('Failed to render HD panorama preview:', error);
    return authOriginalUrl.value;
  } finally {
    if (fullBlobUrl) {
      URL.revokeObjectURL(fullBlobUrl);
    }
  }
}

async function handlePreviewClick() {
  if (!props.preview || !props.url) return;
  previewVisible.value = true;
  if (!previewImageSrc.value) {
    previewLoading.value = true;
    try {
      previewImageSrc.value = await getHdPanoramaPreview();
    } finally {
      previewLoading.value = false;
    }
  }
}
</script>

<template>
  <div
    ref="containerRef"
    class="relative flex mx-auto items-center justify-center overflow-hidden rounded bg-neutral-100 dark:bg-neutral-800"
    :class="{
      'cursor-pointer transition hover:opacity-90':
        preview && thumbnailUrl && !isError,
    }"
    :style="containerStyle"
    @click="handlePreviewClick"
  >
    <!-- 无图或加载失败 -->
    <div
      v-if="!thumbnailUrl || isError"
      class="select-none text-xs text-neutral-400"
    >
      {{ alt || $t('record.capture.drawer.noImage') }}
    </div>

    <!-- 列表展示图：直接使用原生 <img> + loading="lazy"，由浏览器底层多线程并行流式加载 -->
    <template v-else>
      <img
        ref="imgRef"
        :alt="alt || $t('record.capture.drawer.noImage')"
        :height="height"
        :src="thumbnailUrl"
        :style="{ objectFit: fit }"
        :width="width"
        loading="lazy"
        class="h-full w-full select-none"
        @load="onImageLoad"
        @error="() => (isError = true)"
      />

      <!-- 紧贴图片实际渲染区域的 SVG 目标框指示 (纯矢量层覆盖，0 坐标偏移) -->
      <svg
        v-if="normalizedBbox"
        viewBox="0 0 100 100"
        preserveAspectRatio="none"
        class="pointer-events-none absolute"
        :style="overlayStyle"
      >
        <!-- 主体矩形（带微妙投影以保证高低亮背景下的识别度） -->
        <rect
          :x="normalizedBbox[0] * 100"
          :y="normalizedBbox[1] * 100"
          :width="(normalizedBbox[2] - normalizedBbox[0]) * 100"
          :height="(normalizedBbox[3] - normalizedBbox[1]) * 100"
          fill="rgba(14, 165, 233, 0.12)"
          stroke="#0ea5e9"
          stroke-width="1.5"
          vector-effect="non-scaling-stroke"
          style="filter: drop-shadow(0 0 1.5px rgba(0, 0, 0, 0.7));"
        />

        <!-- 四角科技感直角包角 (与边框同色系高亮，去除突兀异色) -->
        <path
          :d="cornerBracketPath"
          fill="none"
          stroke="#38bdf8"
          stroke-width="2.5"
          stroke-linecap="square"
          vector-effect="non-scaling-stroke"
        />
      </svg>

      <!-- 预览加载中的遮罩与微型 Spin 提示 -->
      <div
        v-if="previewLoading"
        class="absolute inset-0 z-10 flex items-center justify-center bg-black/20"
      >
        <Spin size="small" />
      </div>
    </template>

    <!-- 点击放大预览大图（仅在用户点击时按需从高清原图 1080P/4K 生成） -->
    <div v-if="preview && thumbnailUrl && !isError" style="display: none">
      <AntImage
        :preview="{
          visible: previewVisible,
          src: previewImageSrc || authOriginalUrl || thumbnailUrl,
          onVisibleChange: (val: boolean) => (previewVisible = val),
        }"
      />
    </div>
  </div>
</template>
