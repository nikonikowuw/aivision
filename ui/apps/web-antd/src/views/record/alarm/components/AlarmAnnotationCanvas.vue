<script lang="ts" setup>
import type { AlarmRecordApi } from '#/api/core/alarm';

import { computed, ref, watch } from 'vue';

import { $t } from '@vben/locales';

import { Checkbox, Spin } from 'ant-design-vue';

import { getAlarmImageBlobApi } from '#/api/core/alarm';

const props = defineProps<{
  detail: AlarmRecordApi.AlarmRecordDetail | null;
}>();

const imageSrc = ref<string>('');
const imageLoaded = ref<boolean>(false);
const imageLoading = ref<boolean>(false);

const naturalWidth = ref<number>(0);
const naturalHeight = ref<number>(0);

// 图层显示控制
const showRoi = ref(true);
const showMask = ref(true);
const showLine = ref(true);
const showTarget = ref(true);

async function loadImage() {
  if (!props.detail?.imageId) {
    imageSrc.value = '';
    imageLoaded.value = false;
    return;
  }

  imageLoading.value = true;
  imageLoaded.value = false;
  try {
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
  <div class="flex flex-col gap-2">
    <!-- 全景视口容器 -->
    <div
      class="group relative flex min-h-[380px] max-h-[520px] w-full items-center justify-center overflow-hidden rounded-lg border border-border/80 bg-zinc-950/95 shadow-inner"
    >
      <!-- 分辨率与比例标识 -->
      <div
        v-if="imageLoaded && naturalWidth && naturalHeight"
        class="pointer-events-none absolute top-3 left-3 z-10 rounded bg-black/60 px-2 py-0.5 font-mono text-[11px] text-zinc-300 backdrop-blur-sm"
      >
        {{ naturalWidth }} × {{ naturalHeight }}
      </div>

      <div
        v-if="imageLoading"
        class="flex h-72 w-full flex-col items-center justify-center gap-2 text-zinc-400"
      >
        <Spin size="default" />
        <span class="text-xs">{{ $t('common.loading') }}...</span>
      </div>

      <div
        v-else-if="!props.detail?.imageId || !imageSrc"
        class="flex h-72 w-full items-center justify-center text-xs text-zinc-500"
      >
        <span>{{ $t('record.alarm.detail.noImage') }}</span>
      </div>

      <div
        v-else
        class="relative flex h-full w-full items-center justify-center p-2"
      >
        <!-- 紧密贴合图片真实边界的自适应容器，保证标注图层 0 坐标偏移 -->
        <div
          class="relative inline-flex max-h-[500px] max-w-full items-center justify-center overflow-hidden rounded"
        >
          <!-- 底图 -->
          <img
            :src="imageSrc"
            alt="Alarm snapshot"
            class="block max-h-[500px] w-auto max-w-full select-none object-contain transition-transform duration-200"
            @load="onImageLoad"
          />

          <!-- SVG 覆盖层：100% 贴合底图区域 -->
          <svg
            v-if="imageLoaded"
            :viewBox="viewBox"
            class="pointer-events-none absolute inset-0 h-full w-full"
            preserveAspectRatio="none"
          >
            <!-- 规则图层 -->
            <template
              v-for="(rule, idx) in props.detail?.rules || []"
              :key="`rule-${idx}`"
            >
              <!-- ROI 区域: 黄色半透明 -->
              <polygon
                v-if="rule.role === 1 && showRoi"
                :points="getPointsString(rule.points)"
                class="fill-yellow-500/20 stroke-yellow-400 transition-all duration-150"
                stroke-dasharray="6 3"
                stroke-width="2.5"
              />

              <!-- 屏蔽区域: 灰色半透明 -->
              <polygon
                v-else-if="rule.role === 2 && showMask"
                :points="getPointsString(rule.points)"
                class="fill-zinc-800/50 stroke-zinc-400 transition-all duration-150"
                stroke-dasharray="4 4"
                stroke-width="1.5"
              />

              <!-- 警戒线: 橙色折线 -->
              <polyline
                v-else-if="rule.role === 3 && showLine"
                :points="getPointsString(rule.points)"
                class="fill-none stroke-amber-500 transition-all duration-150"
                stroke-width="3.5"
                stroke-linecap="round"
              />
            </template>

            <!-- 触发目标边界框图层: 亮红边框 + 阴影标签 -->
            <g
              v-if="
                showTarget &&
                props.detail?.bbox &&
                props.detail.bbox.length === 4
              "
              class="transition-all duration-150"
            >
              <rect
                :x="getBBoxCoords(props.detail.bbox).x"
                :y="getBBoxCoords(props.detail.bbox).y"
                :width="getBBoxCoords(props.detail.bbox).width"
                :height="getBBoxCoords(props.detail.bbox).height"
                fill="rgba(239, 68, 68, 0.16)"
                stroke="#ef4444"
                stroke-width="2.5"
                style="filter: drop-shadow(0 0 2px rgb(0 0 0 / 70%))"
              />
              <!-- 目标顶部标签底色与文字 -->
              <g
                :transform="`translate(${getBBoxCoords(props.detail.bbox).x}, ${Math.max(0, getBBoxCoords(props.detail.bbox).y - 26)})`"
              >
                <rect
                  :width="
                    Math.max(
                      120,
                      (props.detail.targetLabel || 'Target').length * 9 + 68,
                    )
                  "
                  height="24"
                  rx="4"
                  fill="rgba(24, 24, 27, 0.92)"
                  stroke="#ef4444"
                  stroke-width="1"
                />
                <text
                  x="8"
                  y="16"
                  class="fill-white font-mono text-xs font-semibold select-none"
                  font-size="12"
                >
                  {{ props.detail.targetLabel || 'Target' }}
                  {{ (props.detail.confidence * 100).toFixed(1) }}%
                </text>
              </g>
            </g>
          </svg>
        </div>
      </div>
    </div>

    <!-- 底部交互式图层控制栏与图例 -->
    <div
      class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border/60 bg-muted/40 px-3 py-2 text-xs"
    >
      <span class="font-medium text-foreground/80">
        {{ $t('record.alarm.detail.imageAnnotation') }}
      </span>

      <div class="flex flex-wrap items-center gap-4">
        <!-- ROI 开关 -->
        <label
          class="inline-flex cursor-pointer items-center gap-1.5 select-none hover:opacity-80"
        >
          <Checkbox v-model:checked="showRoi" size="small" />
          <span
            class="inline-block h-2.5 w-2.5 rounded-xs border border-yellow-400 bg-yellow-400/40"
          ></span>
          <span class="text-foreground/90">{{
            $t('record.alarm.detail.legendRoi')
          }}</span>
        </label>

        <!-- 屏蔽区开关 -->
        <label
          class="inline-flex cursor-pointer items-center gap-1.5 select-none hover:opacity-80"
        >
          <Checkbox v-model:checked="showMask" size="small" />
          <span
            class="inline-block h-2.5 w-2.5 rounded-xs border border-zinc-400 bg-zinc-600/50"
          ></span>
          <span class="text-foreground/90">{{
            $t('record.alarm.detail.legendMask')
          }}</span>
        </label>

        <!-- 警戒线开关 -->
        <label
          class="inline-flex cursor-pointer items-center gap-1.5 select-none hover:opacity-80"
        >
          <Checkbox v-model:checked="showLine" size="small" />
          <span class="inline-block h-1 w-3 rounded-full bg-amber-500"></span>
          <span class="text-foreground/90">{{
            $t('record.alarm.detail.legendLine')
          }}</span>
        </label>

        <!-- 目标框开关 -->
        <label
          class="inline-flex cursor-pointer items-center gap-1.5 select-none hover:opacity-80"
        >
          <Checkbox v-model:checked="showTarget" size="small" />
          <span
            class="inline-block h-2.5 w-2.5 rounded-xs border border-red-500 bg-red-500/30"
          ></span>
          <span class="text-foreground/90">{{
            $t('record.alarm.detail.legendTarget')
          }}</span>
        </label>
      </div>
    </div>
  </div>
</template>
