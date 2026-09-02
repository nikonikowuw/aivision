<script lang="ts" setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';

import { useAccessStore } from '@vben/stores';

import { Image as AntImage } from 'ant-design-vue';

import { getAlarmImageBlobApi } from '#/api/core/alarm';

const props = defineProps<{
  imageId?: string;
  bbox?: [number, number, number, number]; // [x1, y1, x2, y2] 归一化坐标
  width?: number;
  height?: number;
  padRatio?: number; // 扩图外延比例，默认 0.15 (15%)
}>();

const accessStore = useAccessStore();
const containerRef = ref<HTMLDivElement | null>(null);
const hdCroppedDataUrl = ref<string>('');
const isVisible = ref<boolean>(true); // 默认直接显示，由浏览器原生并发渲染
let observer: IntersectionObserver | null = null;

watch(
  () => [props.imageId, props.bbox],
  () => {
    hdCroppedDataUrl.value = '';
    previewImageSrc.value = '';
  },
);

// 使用原生全景缩略图直链（带 Token 鉴权，由浏览器 C++ 内核多线程流式懒加载）
const thumbnailUrl = computed(() => {
  if (!props.imageId) return '';
  const token = accessStore.accessToken;
  const baseUrl = `/api/record/images/${encodeURIComponent(props.imageId)}?type=thumb`;
  return token ? `${baseUrl}&token=${encodeURIComponent(token)}` : baseUrl;
});

// 计算基于 CSS background-image + background-size + background-position 的零 JS 损耗目标切图展示
const cropStyle = computed(() => {
  if (!thumbnailUrl.value || !props.bbox || props.bbox.length < 4) {
    return {};
  }
  const [x1, y1, x2, y2] = props.bbox;
  let minX = Math.min(x1, x2);
  let minY = Math.min(y1, y2);
  let maxX = Math.max(x1, x2);
  let maxY = Math.max(y1, y2);

  const pad = props.padRatio ?? 0.15;
  const boxW = Math.max(0.001, maxX - minX);
  const boxH = Math.max(0.001, maxY - minY);
  minX = Math.max(0, minX - boxW * pad);
  minY = Math.max(0, minY - boxH * pad);
  maxX = Math.min(1, maxX + boxW * pad);
  maxY = Math.min(1, maxY + boxH * pad);

  const finalW = Math.max(0.001, maxX - minX);
  const finalH = Math.max(0.001, maxY - minY);

  const bgSizeX = (100 / finalW).toFixed(2);
  const bgSizeY = (100 / finalH).toFixed(2);

  const posX = finalW >= 1 ? 0 : ((minX / (1 - finalW)) * 100).toFixed(2);
  const posY = finalH >= 1 ? 0 : ((minY / (1 - finalH)) * 100).toFixed(2);

  return {
    backgroundImage: `url("${thumbnailUrl.value}")`,
    backgroundSize: `${bgSizeX}% ${bgSizeY}%`,
    backgroundPosition: `${posX}% ${posY}%`,
    backgroundRepeat: 'no-repeat',
  };
});

/**
 * 用户点击放大/查看特写时，按需从 1080P/4K 高清原图无损抠取真实超清特写
 */
async function getHdCroppedPreview(): Promise<string> {
  if (hdCroppedDataUrl.value) return hdCroppedDataUrl.value;
  if (!props.imageId || !props.bbox || props.bbox.length < 4) return '';

  try {
    const fullBlob = await getAlarmImageBlobApi(props.imageId, false);
    if (!fullBlob) return '';

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
      return '';
    }

    const [x1, y1, x2, y2] = props.bbox;
    let minX = Math.min(x1, x2);
    let minY = Math.min(y1, y2);
    let maxX = Math.max(x1, x2);
    let maxY = Math.max(y1, y2);

    const pad = props.padRatio ?? 0.15;
    const boxW = maxX - minX;
    const boxH = maxY - minY;
    minX = Math.max(0, minX - boxW * pad);
    minY = Math.max(0, minY - boxH * pad);
    maxX = Math.min(1, maxX + boxW * pad);
    maxY = Math.min(1, maxY + boxH * pad);

    const srcX = minX * hdW;
    const srcY = minY * hdH;
    const srcW = Math.max(1, (maxX - minX) * hdW);
    const srcH = Math.max(1, (maxY - minY) * hdH);

    const offscreenCanvas = document.createElement('canvas');
    offscreenCanvas.width = srcW;
    offscreenCanvas.height = srcH;
    const offscreenCtx = offscreenCanvas.getContext('2d');
    if (!offscreenCtx) {
      URL.revokeObjectURL(fullBlobUrl);
      return '';
    }

    // 1:1 无损无压缩将原图目标像素绘制到全尺寸 Canvas
    offscreenCtx.drawImage(hdImg, srcX, srcY, srcW, srcH, 0, 0, srcW, srcH);
    hdCroppedDataUrl.value = offscreenCanvas.toDataURL('image/jpeg', 0.95);
    URL.revokeObjectURL(fullBlobUrl);
    return hdCroppedDataUrl.value;
  } catch (e) {
    console.error('Failed to crop HD target:', e);
    return '';
  }
}

onMounted(() => {
  if (typeof IntersectionObserver !== 'undefined' && containerRef.value) {
    observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting) {
          isVisible.value = true;
          observer?.disconnect();
          observer = null;
        }
      },
      { rootMargin: '400px' },
    );
    observer.observe(containerRef.value);
  } else {
    isVisible.value = true;
  }
});

onUnmounted(() => {
  observer?.disconnect();
  observer = null;
});

const previewVisible = ref<boolean>(false);
const previewImageSrc = ref<string>('');

async function handlePreviewClick() {
  previewVisible.value = true;
  previewImageSrc.value = await getHdCroppedPreview();
}
</script>

<template>
  <div
    ref="containerRef"
    :key="props.imageId"
    class="relative flex mx-auto cursor-pointer items-center justify-center overflow-hidden rounded border border-border bg-muted/40 transition hover:opacity-90"
    :style="{
      width: (props.width || 64) + 'px',
      height: (props.height || 64) + 'px',
    }"
    @click="handlePreviewClick"
  >
    <span v-if="!thumbnailUrl" class="text-[10px] text-muted-foreground">
      无图
    </span>
    <!-- 列表展示：使用视口懒加载 + CSS GPU 级视口裁剪（0 JS 离屏 Canvas，瞬间出图） -->
    <div
      v-else-if="isVisible"
      class="h-full w-full"
      :style="cropStyle"
    />

    <!-- 点击放大预览弹窗（无损从 1080P/4K 高清原图提取） -->
    <div style="display: none">
      <AntImage
        :preview="{
          visible: previewVisible,
          src: previewImageSrc,
          onVisibleChange: (val: boolean) => (previewVisible = val),
        }"
      />
    </div>
  </div>
</template>
