<script lang="ts" setup>
import type { AlarmRecordApi } from '#/api/core/alarm';

import { computed, ref, watch } from 'vue';

import { getAlarmImageBlobApi } from '#/api/core/alarm';

const props = defineProps<{
  detail: AlarmRecordApi.AlarmRecordDetail | null;
}>();

const imageSrc = ref<string>('');
const imageLoaded = ref<boolean>(false);
const imageLoading = ref<boolean>(false);

const naturalWidth = ref<number>(0);
const naturalHeight = ref<number>(0);

async function loadImage() {
  if (!props.detail?.imageId) {
    imageSrc.value = '';
    imageLoaded.value = false;
    return;
  }

  imageLoading.value = true;
  imageLoaded.value = false;
  try {
    // 详情弹窗高保真标注：请求 1080P/4K 原图 (isThumbnail = false)
    // 每次切换 detail 时，先清空并释放旧 ObjectURL，避免显示上一张图片的残留
    if (imageSrc.value) {
      URL.revokeObjectURL(imageSrc.value);
      imageSrc.value = '';
    }
    const blob = await getAlarmImageBlobApi(props.detail.imageId, false);
    if (blob && props.detail?.imageId) {
      imageSrc.value = URL.createObjectURL(blob);
    }
  } catch (error) {
    console.error('Failed to load alarm image:', error);
  } finally {
    imageLoading.value = false;
  }
}

function onImageLoad(e: Event) {
  const target = e.target as HTMLImageElement;
  naturalWidth.value = target.naturalWidth;
  naturalHeight.value = target.naturalHeight;
  imageLoaded.value = true;
}

watch(
  () => props.detail?.imageId,
  () => {
    loadImage();
  },
  { immediate: true },
);

// 将归一化坐标转换为 SVG viewBox 坐标 (以 naturalWidth / naturalHeight 为基准)
const viewBox = computed(() => {
  const w = naturalWidth.value || 1920;
  const h = naturalHeight.value || 1080;
  return `0 0 ${w} ${h}`;
});

function getPointsString(points: AlarmRecordApi.DetectionPoint[]) {
  const w = naturalWidth.value || 1920;
  const h = naturalHeight.value || 1080;
  return points.map((p) => `${p.x * w},${p.y * h}`).join(' ');
}

function getBBoxCoords(bbox: [number, number, number, number]) {
  const w = naturalWidth.value || 1920;
  const h = naturalHeight.value || 1080;
  const [x1, y1, x2, y2] = bbox;
  return {
    x: Math.min(x1, x2) * w,
    y: Math.min(y1, y2) * h,
    width: Math.abs(x2 - x1) * w,
    height: Math.abs(y2 - y1) * h,
  };
}
</script>

<template>
  <div class="relative flex w-full flex-col items-center justify-center overflow-hidden rounded border border-border bg-black/90 p-1">
    <div
      v-if="imageLoading"
      class="flex h-64 w-full items-center justify-center text-muted-foreground"
    >
      <span>{{ $t('common.loading') }}...</span>
    </div>

    <div
      v-else-if="!props.detail?.imageId || !imageSrc"
      class="flex h-64 w-full items-center justify-center text-muted-foreground"
    >
      <span>{{ $t('record.alarm.detail.noImage') }}</span>
    </div>

    <div v-else class="relative inline-block max-h-[500px] max-w-full overflow-hidden">
      <!-- 底图 -->
      <img
        :src="imageSrc"
        alt="Alarm snapshot"
        class="block max-h-[500px] max-w-full object-contain"
        @load="onImageLoad"
      />

      <!-- SVG 覆盖层 -->
      <svg
        v-if="imageLoaded"
        :viewBox="viewBox"
        class="pointer-events-none absolute inset-0 h-full w-full"
        preserveAspectRatio="none"
      >
        <!-- 规则图层 -->
        <g v-for="(rule, idx) in props.detail?.rules || []" :key="'rule-' + idx">
          <!-- ROI 区域: 黄色半透明 -->
          <polygon
            v-if="rule.role === 1"
            :points="getPointsString(rule.points)"
            class="fill-yellow-500/20 stroke-yellow-400"
            stroke-dasharray="4 2"
            stroke-width="3"
          />

          <!-- 屏蔽区域: 灰色半透明 -->
          <polygon
            v-else-if="rule.role === 2"
            :points="getPointsString(rule.points)"
            class="fill-gray-600/40 stroke-gray-400"
            stroke-dasharray="3 3"
            stroke-width="2"
          />

          <!-- 警戒线: 橙色折线 -->
          <polyline
            v-else-if="rule.role === 3"
            :points="getPointsString(rule.points)"
            class="fill-none stroke-amber-500"
            stroke-width="4"
          />
        </g>

        <!-- 单目标边界框图层: 红色边框 + 标签 -->
        <g v-if="props.detail?.bbox && props.detail.bbox.length === 4">
          <rect
            :x="getBBoxCoords(props.detail.bbox).x"
            :y="getBBoxCoords(props.detail.bbox).y"
            :width="getBBoxCoords(props.detail.bbox).width"
            :height="getBBoxCoords(props.detail.bbox).height"
            class="fill-red-500/20 stroke-red-500"
            stroke-width="3"
          />
          <text
            :x="getBBoxCoords(props.detail.bbox).x + 4"
            :y="Math.max(16, getBBoxCoords(props.detail.bbox).y - 6)"
            class="fill-red-400 text-sm font-bold drop-shadow"
            font-size="16"
          >
            {{ props.detail.targetLabel || 'Target' }} ({{ (props.detail.confidence * 100).toFixed(1) }}%)
          </text>
        </g>
      </svg>
    </div>
  </div>
</template>
