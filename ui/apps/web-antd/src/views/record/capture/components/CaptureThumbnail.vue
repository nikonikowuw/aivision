<script lang="ts" setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue';

import { $t } from '@vben/locales';
import { useAccessStore } from '@vben/stores';

import { Image as AntImage, Spin } from 'ant-design-vue';

const props = withDefaults(
  defineProps<{
    alt?: string;
    bbox?: [number, number, number, number] | number[];
    fit?: 'contain' | 'cover' | 'fill';
    height?: number;
    preview?: boolean;
    url?: string;
    width?: number;
  }>(),
  {
    alt: '',
    bbox: undefined,
    fit: 'cover',
    height: 48,
    preview: true,
    url: '',
    width: 96,
  },
);

const accessStore = useAccessStore();
const imageSrc = ref<string>('');
const previewSrc = ref<string>('');
const loading = ref<boolean>(false);
let objectUrlToRevoke = '';

const authUrl = computed(() => {
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

function cleanupObjectUrl() {
  if (objectUrlToRevoke) {
    URL.revokeObjectURL(objectUrlToRevoke);
    objectUrlToRevoke = '';
  }
}

onBeforeUnmount(() => {
  cleanupObjectUrl();
});

async function generateBboxPreview(
  src: string,
  box: [number, number, number, number],
): Promise<string> {
  try {
    const img = new Image();
    img.crossOrigin = 'anonymous';
    await new Promise((resolve, reject) => {
      img.addEventListener('load', resolve, { once: true });
      img.addEventListener('error', reject, { once: true });
      img.src = src;
    });

    const w = img.naturalWidth || img.width;
    const h = img.naturalHeight || img.height;
    if (!w || !h) return src;

    const canvas = document.createElement('canvas');
    canvas.width = w;
    canvas.height = h;
    const ctx = canvas.getContext('2d');
    if (!ctx) return src;

    ctx.drawImage(img, 0, 0, w, h);

    const [minX, minY, maxX, maxY] = box;
    const px = minX * w;
    const py = minY * h;
    const pw = (maxX - minX) * w;
    const ph = (maxY - minY) * h;

    ctx.save();
    ctx.lineWidth = Math.max(3, Math.round(w / 400));
    ctx.strokeStyle = '#1890ff';
    ctx.fillStyle = 'rgba(24, 144, 255, 0.18)';
    ctx.fillRect(px, py, pw, ph);
    ctx.strokeRect(px, py, pw, ph);

    // 绘制微型科技感四角包角高亮
    const cornerLen = Math.min(pw, ph) * 0.2;
    ctx.lineWidth = Math.max(4, Math.round(w / 300));
    ctx.strokeStyle = '#52c41a';

    // 左上
    ctx.beginPath();
    ctx.moveTo(px, py + cornerLen);
    ctx.lineTo(px, py);
    ctx.lineTo(px + cornerLen, py);
    ctx.stroke();

    // 右上
    ctx.beginPath();
    ctx.moveTo(px + pw - cornerLen, py);
    ctx.lineTo(px + pw, py);
    ctx.lineTo(px + pw, py + cornerLen);
    ctx.stroke();

    // 左下
    ctx.beginPath();
    ctx.moveTo(px, py + ph - cornerLen);
    ctx.lineTo(px, py + ph);
    ctx.lineTo(px + cornerLen, py + ph);
    ctx.stroke();

    // 右下
    ctx.beginPath();
    ctx.moveTo(px + pw - cornerLen, py + ph);
    ctx.lineTo(px + pw, py + ph);
    ctx.lineTo(px + pw, py + ph - cornerLen);
    ctx.stroke();

    ctx.restore();

    return canvas.toDataURL('image/jpeg', 0.95);
  } catch {
    return src;
  }
}

watch(
  [() => props.url, normalizedBbox],
  async ([newUrl, box]) => {
    cleanupObjectUrl();
    if (!newUrl) {
      imageSrc.value = '';
      previewSrc.value = '';
      return;
    }
    loading.value = true;
    try {
      const resp = await fetch(newUrl, {
        headers: {
          Authorization: `Bearer ${accessStore.accessToken || ''}`,
        },
      });
      if (!resp.ok) {
        throw new Error(`HTTP ${resp.status}`);
      }
      const blob = await resp.blob();
      const objectUrl = URL.createObjectURL(blob);
      objectUrlToRevoke = objectUrl;
      imageSrc.value = objectUrl;

      previewSrc.value = box
        ? await generateBboxPreview(objectUrl, box)
        : objectUrl;
    } catch {
      imageSrc.value = authUrl.value;
      previewSrc.value = authUrl.value;
    } finally {
      loading.value = false;
    }
  },
  { immediate: true },
);
</script>

<template>
  <div
    class="relative flex items-center justify-center overflow-hidden rounded bg-neutral-100 dark:bg-neutral-800"
    :style="{ width: `${width}px`, height: `${height}px` }"
  >
    <Spin v-if="loading" size="small" />
    <template v-else-if="imageSrc">
      <AntImage
        :alt="alt || $t('record.capture.drawer.noImage')"
        :height="height"
        :preview="
          preview
            ? {
                src: previewSrc || imageSrc,
              }
            : false
        "
        :src="imageSrc"
        :style="{ objectFit: fit }"
        :width="width"
      />
      <!-- 缩略图上的微型目标框指示 (仅当有 bbox 且非全屏预览时覆盖) -->
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
          fill="rgba(24, 144, 255, 0.25)"
          stroke="#1890ff"
          stroke-width="2"
        />
      </svg>
    </template>
    <div v-else class="select-none text-xs text-neutral-400">
      {{ $t('record.capture.drawer.noImage') }}
    </div>
  </div>
</template>
