<script lang="ts" setup>
import type { VxeTableGridOptions } from '#/adapter/vxe-table';
import type { FaceObservationApi } from '#/api';

import { ref } from 'vue';

import { Page, useVbenModal } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';
import { $t } from '@vben/locales';
import { formatDateTime } from '@vben/utils';

import {
  Button,
  message,
  Tag,
} from 'ant-design-vue';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  getCameraPageApi,
  getFaceObservationDetailApi,
  getFaceObservationListApi,
  getPersonFaceImageUrl,
} from '#/api';
import { getTodayRange } from '#/utils/date';

import CaptureThumbnail from '../capture/components/CaptureThumbnail.vue';
import FaceCandidatesTable from '../capture/components/FaceCandidatesTable.vue';
import FaceThumbnail from '../../resource/person/components/FaceThumbnail.vue';

const currentDetail = ref<FaceObservationApi.FaceObservationItem | null>(null);

const [DetailModal, detailModalApi] = useVbenModal({
  class: 'w-[1120px] max-w-[96vw]',
  fullscreenButton: true,
  onConfirm: () => {
    detailModalApi.close();
  },
  showCancelButton: false,
  title: $t('record.face.detail.title'),
});

const gridOptions: VxeTableGridOptions<FaceObservationApi.FaceObservationItem> =
  {
    rowConfig: {
      keyField: 'id',
      isHover: true,
    },
    columns: [
      {
        type: 'seq',
        title: $t('system.common.index'),
        width: 60,
        align: 'center',
      },
      {
        field: 'faceCrop',
        title: $t('record.face.columns.faceCrop'),
        width: 88,
        align: 'center',
        slots: { default: 'faceCrop' },
      },
      {
        field: 'panorama',
        title: $t('record.face.columns.panorama'),
        width: 96,
        align: 'center',
        slots: { default: 'panorama' },
      },
      {
        field: 'galleryFace',
        title: $t('record.face.columns.galleryFace'),
        width: 88,
        align: 'center',
        slots: { default: 'galleryFace' },
      },
      {
        field: 'personName',
        title: $t('record.face.columns.personName'),
        minWidth: 120,
        slots: { default: 'personName' },
      },
      {
        field: 'personId',
        title: $t('record.face.columns.personId'),
        minWidth: 140,
        align: 'center',
        slots: { default: 'personId' },
      },
      {
        field: 'similarity',
        title: $t('record.face.columns.similarity'),
        width: 100,
        align: 'center',
        slots: { default: 'similarity' },
      },
      {
        field: 'cameraName',
        title: $t('record.face.columns.cameraName'),
        minWidth: 140,
      },
      {
        field: 'observedAt',
        formatter: 'formatDateTime',
        title: $t('record.face.columns.observedAt'),
        width: 170,
        align: 'center',
      },
      {
        field: 'actions',
        fixed: 'right',
        showOverflow: false,
        slots: { default: 'actions' },
        title: $t('system.common.action'),
        width: 90,
        align: 'center',
      },
    ],
    pagerConfig: {
      enabled: true,
    },
    proxyConfig: {
      ajax: {
        query: async ({ page }, formValues) => {
          const { maxSimilarity, minSimilarity, timeRange, ...rest } =
            formValues || {};
          let startTime: string | undefined;
          let endTime: string | undefined;
          if (timeRange && Array.isArray(timeRange) && timeRange.length === 2) {
            startTime = timeRange[0];
            endTime = timeRange[1];
          }

          return await getFaceObservationListApi({
            endTime,
            maxSimilarity: toRatio(maxSimilarity),
            minSimilarity: toRatio(minSimilarity),
            page: page.currentPage,
            pageSize: page.pageSize,
            startTime,
            ...rest,
          });
        },
      },
    },
  };

const [Grid] = useVbenVxeGrid({
  formOptions: {
    commonConfig: {
      labelWidth: 80,
    },
    schema: [
      {
        component: 'ApiSelect',
        componentProps: {
          allowClear: true,
          api: async () => {
            const res = await getCameraPageApi({ page: 1, pageSize: 100 });
            return res.items.map((camera) => ({
              label: camera.name,
              value: camera.cameraId,
            }));
          },
          placeholder: $t('record.face.filter.cameraPlaceholder'),
        },
        fieldName: 'cameraId',
        label: $t('record.face.filter.camera'),
      },
      {
        component: 'Input',
        componentProps: {
          placeholder: $t('record.face.filter.personNamePlaceholder'),
        },
        fieldName: 'personName',
        label: $t('record.face.filter.personName'),
      },
      {
        component: 'Input',
        componentProps: {
          placeholder: $t('record.face.filter.personIdPlaceholder'),
        },
        fieldName: 'personId',
        label: $t('record.face.filter.personId'),
      },
      {
        component: 'RangePicker',
        componentProps: {
          showTime: true,
          valueFormat: 'YYYY-MM-DDTHH:mm:ssZ',
        },
        defaultValue: getTodayRange(),
        fieldName: 'timeRange',
        label: $t('record.face.filter.timeRange'),
      },
      {
        component: 'InputNumber',
        componentProps: {
          addonAfter: '%',
          max: 100,
          min: 0,
          precision: 1,
        },
        fieldName: 'minSimilarity',
        label: $t('record.face.filter.minSimilarity'),
      },
      {
        component: 'InputNumber',
        componentProps: {
          addonAfter: '%',
          max: 100,
          min: 0,
          precision: 1,
        },
        fieldName: 'maxSimilarity',
        label: $t('record.face.filter.maxSimilarity'),
      },
    ],
  },
  gridOptions,
});

function toRatio(value: unknown): number | undefined {
  if (typeof value !== 'number' || !Number.isFinite(value)) return undefined;
  return value / 100;
}

async function handleViewDetail(row: FaceObservationApi.FaceObservationItem) {
  try {
    detailModalApi.open();
    currentDetail.value = row;
    const full = await getFaceObservationDetailApi(row.id);
    if (full) currentDetail.value = full;
  } catch (error) {
    console.error('Failed to load face observation detail:', error);
  }
}

function fallbackCopy(text: string) {
  try {
    const textArea = document.createElement('textarea');
    textArea.value = text;
    textArea.style.position = 'fixed';
    textArea.style.opacity = '0';
    document.body.appendChild(textArea);
    textArea.focus();
    textArea.select();
    const successful = document.execCommand('copy');
    document.body.removeChild(textArea);
    if (successful) {
      message.success($t('record.face.detail.copySuccess'));
      return;
    }
  } catch (e) {
    console.error('Fallback copy failed:', e);
  }
  message.info(text);
}

function copyEventId(eventId?: string) {
  if (!eventId) return;
  if (navigator.clipboard?.writeText) {
    navigator.clipboard
      .writeText(eventId)
      .then(() => message.success($t('record.face.detail.copySuccess')))
      .catch(() => fallbackCopy(eventId));
  } else {
    fallbackCopy(eventId);
  }
}

function formatSimilarity(value?: number): string {
  return typeof value === 'number' ? `${(value * 100).toFixed(1)}%` : '-';
}

function getSimilarityTagColor(similarity?: number): string {
  if (typeof similarity !== 'number') return 'default';
  if (similarity >= 0.9) return 'green';
  if (similarity >= 0.75) return 'blue';
  if (similarity >= 0.6) return 'orange';
  return 'red';
}
</script>

<template>
  <Page auto-content-height>
    <Grid>
      <template #faceCrop="{ row }">
        <CaptureThumbnail
          fit="cover"
          :height="56"
          :url="row.faceImageUrl"
          :width="56"
          :alt="$t('record.face.columns.faceCrop')"
          class="mx-auto"
        />
      </template>

      <template #panorama="{ row }">
        <CaptureThumbnail
          fit="contain"
          :height="41"
          :url="row.panoramaImageUrl"
          :bbox="row.bbox"
          :width="72"
          :alt="$t('record.face.columns.panorama')"
          class="mx-auto aspect-video"
        />
      </template>

      <template #galleryFace="{ row }">
        <FaceThumbnail
          v-if="row.personId && row.faceId"
          :size="56"
          :url="getPersonFaceImageUrl(row.personId, row.faceId, 'aligned')"
          :alt="row.personName || 'Gallery'"
          class="mx-auto"
        />
        <div
          v-else
          class="flex h-14 w-14 mx-auto items-center justify-center rounded border border-neutral-200 bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-800 text-neutral-400"
        >
          <IconifyIcon icon="lucide:user" class="size-6" />
        </div>
      </template>

      <template #personName="{ row }">
        <span class="font-medium">{{ row.personName || '-' }}</span>
      </template>

      <template #personId="{ row }">
        <span v-if="row.personId" class="font-mono text-xs">
          {{ row.personId }}
        </span>
        <span v-else>-</span>
      </template>

      <template #similarity="{ row }">
        <Tag :color="getSimilarityTagColor(row.similarity)">
          {{ formatSimilarity(row.similarity) }}
        </Tag>
      </template>

      <template #actions="{ row }">
        <Button size="small" type="link" @click="handleViewDetail(row)">
          {{ $t('system.common.detail') }}
        </Button>
      </template>
    </Grid>

    <DetailModal>
      <div v-if="currentDetail" class="space-y-4">
        <!-- 1. 顶部 1:1 人脸比对核验区（居中对称展示） -->
        <div
          class="relative flex items-center justify-center overflow-hidden rounded-xl border border-border/80 bg-gradient-to-b from-card via-card to-muted/20 py-4.5 px-6 shadow-sm"
        >
          <!-- 1:1 现场抓拍 vs 底库样本 居中对称展示 -->
          <div class="flex items-center justify-center gap-4 sm:gap-8">
            <!-- 现场抓拍特写 -->
            <div class="flex flex-col items-center gap-1.5">
              <div
                class="relative overflow-hidden rounded-xl border-2 border-primary/50 bg-neutral-950 p-1 shadow-md transition hover:border-primary"
              >
                <CaptureThumbnail
                  fit="cover"
                  :height="96"
                  :original="true"
                  :url="currentDetail.faceImageUrl"
                  :width="96"
                  :alt="$t('record.face.columns.faceCrop')"
                  class="rounded-lg"
                />
              </div>
              <span
                class="rounded-md bg-primary/10 px-2.5 py-0.5 text-xs font-semibold text-primary"
              >
                {{ $t('record.face.columns.faceCrop') }}
              </span>
            </div>

            <!-- 中间：置信度连线与 AI 比对结果指示器 -->
            <div class="flex flex-col items-center gap-1.5 px-2">
              <div class="flex items-center gap-2 sm:gap-3">
                <div class="h-0.5 w-6 bg-border sm:w-12"></div>
                <div
                  class="flex flex-col items-center justify-center rounded-2xl border px-4 py-2 shadow-xs"
                  :class="[
                    currentDetail.similarity >= 0.85
                      ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
                      : currentDetail.similarity >= 0.7
                        ? 'border-blue-500/30 bg-blue-500/10 text-blue-600 dark:text-blue-400'
                        : 'border-amber-500/30 bg-amber-500/10 text-amber-600 dark:text-amber-400',
                  ]"
                >
                  <span
                    class="font-mono text-2xl font-black tracking-tight"
                  >
                    {{ (currentDetail.similarity * 100).toFixed(1) }}%
                  </span>
                  <span
                    class="text-[11px] font-bold uppercase tracking-wider"
                  >
                    {{
                      currentDetail.similarity >= 0.8
                        ? $t('record.face.detail.matchPass')
                        : $t('record.face.detail.matchWarn')
                    }}
                  </span>
                </div>
                <div class="h-0.5 w-6 bg-border sm:w-12"></div>
              </div>
              <span class="text-xs font-medium text-muted-foreground">
                {{ $t('record.face.columns.similarity') }}
              </span>
            </div>

            <!-- 底库注册样本照 -->
            <div class="flex flex-col items-center gap-1.5">
              <div
                class="relative overflow-hidden rounded-xl border-2 border-border/80 bg-neutral-950 p-1 shadow-md transition hover:border-primary/60"
              >
                <FaceThumbnail
                  v-if="currentDetail.personId && currentDetail.faceId"
                  :size="96"
                  :url="
                    getPersonFaceImageUrl(
                      currentDetail.personId,
                      currentDetail.faceId,
                      'aligned',
                    )
                  "
                  :alt="currentDetail.personName || 'Gallery'"
                  class="rounded-lg"
                />
                <div
                  v-else
                  class="flex h-[96px] w-[96px] items-center justify-center rounded-lg bg-neutral-900 text-neutral-400"
                >
                  <IconifyIcon icon="lucide:user" class="size-10" />
                </div>
              </div>
              <span
                class="rounded-md bg-muted px-2.5 py-0.5 text-xs font-semibold text-muted-foreground"
              >
                {{ $t('record.face.columns.galleryFace') }}
              </span>
            </div>
          </div>
        </div>

        <!-- 2. 中部：全景抓拍大图（左 50%）+ 结构化信息（右 50%）完全等宽高对称 -->
        <div class="grid grid-cols-1 gap-4 lg:grid-cols-2 items-stretch">
          <!-- 左侧：全景抓拍大图与目标框 (50% 宽度，等高卡片) -->
          <div
            class="flex h-full flex-col justify-between rounded-xl border border-border/80 bg-card p-4 shadow-sm"
          >
            <div>
              <div class="mb-3 flex items-center justify-between border-b border-border/60 pb-2.5">
                <div class="flex items-center gap-2">
                  <IconifyIcon icon="lucide:image" class="size-4 text-primary" />
                  <span class="text-xs font-semibold text-foreground">
                    {{ $t('record.face.columns.panorama') }}
                  </span>
                  <span class="rounded bg-muted px-1.5 py-0.5 text-[10px] font-mono text-muted-foreground">
                    16:9
                  </span>
                </div>
                <Tag
                  :color="currentDetail.timeSynced ? 'green' : 'orange'"
                  class="text-[11px]"
                >
                  {{
                    currentDetail.timeSynced
                      ? $t('record.face.detail.synced')
                      : $t('record.face.detail.notSynced')
                  }}
                </Tag>
              </div>

              <!-- 全景抓拍原生 16:9 物理画面视口 (带目标 BBox 标注) -->
              <div
                class="group relative aspect-video w-full items-center justify-center overflow-hidden rounded-lg border border-border/60 bg-zinc-950 shadow-inner"
              >
                <CaptureThumbnail
                  fit="contain"
                  :original="true"
                  :url="currentDetail.panoramaImageUrl"
                  :bbox="currentDetail.bbox"
                  :alt="$t('record.face.columns.panorama')"
                  class="h-full w-full aspect-video"
                />
              </div>
            </div>

            <div class="mt-2.5 text-center text-[11px] text-muted-foreground">
              {{ $t('record.alarm.detail.cropTip') }}
            </div>
          </div>

          <!-- 右侧：结构化元数据面板 (50% 宽度，等高卡片) -->
          <div
            class="flex h-full flex-col justify-between rounded-xl border border-border/80 bg-card p-4 shadow-sm"
          >
            <div class="mb-3 flex items-center gap-2 border-b border-border/60 pb-2.5">
              <IconifyIcon icon="lucide:info" class="size-4 text-primary" />
              <span class="text-xs font-semibold text-foreground">
                {{ $t('record.face.detail.eventInfo') }}
              </span>
            </div>

            <div class="flex flex-1 flex-col justify-between divide-y divide-dashed divide-border/40 py-0.5">
              <!-- 1. 人员姓名 -->
              <div class="flex items-center py-1.5">
                <div class="flex items-center shrink-0">
                  <span class="meta-label">{{ $t('record.face.columns.personName') }}</span>
                  <span class="meta-colon">：</span>
                </div>
                <div class="flex-1 flex items-center justify-center text-center font-semibold text-foreground text-[13px]">
                  {{ currentDetail.personName || '-' }}
                </div>
              </div>

              <!-- 2. 人员工号/ID -->
              <div class="flex items-center py-1.5">
                <div class="flex items-center shrink-0">
                  <span class="meta-label">{{ $t('record.face.columns.personId') }}</span>
                  <span class="meta-colon">：</span>
                </div>
                <div class="flex-1 flex items-center justify-center text-center">
                  <span class="font-mono text-xs font-medium text-foreground bg-muted/60 px-2 py-0.5 rounded border border-border/40">
                    {{ currentDetail.personId || '-' }}
                  </span>
                </div>
              </div>

              <!-- 3. 人脸样本 ID -->
              <div class="flex items-center py-1.5">
                <div class="flex items-center shrink-0">
                  <span class="meta-label">{{ $t('record.face.detail.faceId') }}</span>
                  <span class="meta-colon">：</span>
                </div>
                <div class="flex-1 flex items-center justify-center text-center">
                  <span
                    v-if="currentDetail.faceId"
                    class="font-mono text-xs text-muted-foreground"
                  >
                    {{ currentDetail.faceId }}
                  </span>
                  <span v-else class="text-muted-foreground">-</span>
                </div>
              </div>

              <!-- 4. 相似度 -->
              <div class="flex items-center py-1.5">
                <div class="flex items-center shrink-0">
                  <span class="meta-label">{{ $t('record.face.columns.similarity') }}</span>
                  <span class="meta-colon">：</span>
                </div>
                <div class="flex-1 flex items-center justify-center text-center">
                  <Tag
                    :color="getSimilarityTagColor(currentDetail.similarity)"
                    class="font-mono font-semibold px-2.5 py-0.5 text-xs rounded-md m-0"
                  >
                    {{ (currentDetail.similarity * 100).toFixed(1) }}%
                  </Tag>
                </div>
              </div>

              <!-- 5. 抓拍摄像头 -->
              <div class="flex items-center py-1.5">
                <div class="flex items-center shrink-0">
                  <span class="meta-label">{{ $t('record.face.columns.cameraName') }}</span>
                  <span class="meta-colon">：</span>
                </div>
                <div class="flex-1 flex items-center justify-center text-center font-medium text-foreground/90 text-xs">
                  {{
                    currentDetail.cameraName ||
                    currentDetail.cameraId ||
                    '-'
                  }}
                </div>
              </div>

              <!-- 6. 识别时间 -->
              <div class="flex items-center py-1.5">
                <div class="flex items-center shrink-0">
                  <span class="meta-label">{{ $t('record.face.columns.observedAt') }}</span>
                  <span class="meta-colon">：</span>
                </div>
                <div class="flex-1 flex items-center justify-center text-center font-mono text-xs text-foreground/80">
                  {{ formatDateTime(currentDetail.observedAt) || '-' }}
                </div>
              </div>

              <!-- 7. Track ID -->
              <div class="flex items-center py-1.5">
                <div class="flex items-center shrink-0">
                  <span class="meta-label">{{ $t('record.face.detail.trackId') }}</span>
                  <span class="meta-colon">：</span>
                </div>
                <div class="flex-1 flex items-center justify-center text-center">
                  <Tag
                    v-if="currentDetail.trackId"
                    color="purple"
                    class="font-mono text-xs px-2 py-0.2 rounded m-0"
                  >
                    #{{ currentDetail.trackId }}
                  </Tag>
                  <span v-else class="text-muted-foreground">-</span>
                </div>
              </div>

              <!-- 8. 识别算法 -->
              <div class="flex items-center py-1.5">
                <div class="flex items-center shrink-0">
                  <span class="meta-label">{{ $t('record.face.detail.algorithm') }}</span>
                  <span class="meta-colon">：</span>
                </div>
                <div class="flex-1 flex items-center justify-center text-center gap-1.5">
                  <span class="font-mono text-xs text-foreground font-medium">
                    {{ currentDetail.algorithmId || '-' }}
                  </span>
                  <span
                    v-if="currentDetail.algorithmVersion"
                    class="rounded bg-primary/10 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-primary"
                  >
                    v{{ currentDetail.algorithmVersion }}
                  </span>
                </div>
              </div>

              <!-- 9. 事件标识 -->
              <div class="flex items-center py-1.5">
                <div class="flex items-center shrink-0">
                  <span class="meta-label">{{ $t('record.face.detail.eventId') }}</span>
                  <span class="meta-colon">：</span>
                </div>
                <div class="flex-1 flex items-center justify-center text-center">
                  <div class="inline-flex items-center justify-center gap-1.5">
                    <span
                      class="font-mono text-xs text-muted-foreground select-all rounded bg-muted/40 px-2 py-0.5 border border-border/40"
                    >
                      {{ currentDetail.eventId }}
                    </span>
                    <Button
                      type="text"
                      size="small"
                      class="h-6 w-6 shrink-0 p-0 text-muted-foreground hover:text-primary transition-colors flex items-center justify-center"
                      @click="copyEventId(currentDetail.eventId)"
                    >
                      <IconifyIcon icon="lucide:copy" class="size-3.5" />
                    </Button>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- 3. 底部：Top-5 候选人员比对溯源 -->
        <div class="rounded-xl border border-border/80 bg-card p-3.5 shadow-sm">
          <div
            class="mb-3 flex items-center justify-between border-b border-border/60 pb-2"
          >
            <div class="flex items-center gap-2">
              <IconifyIcon icon="lucide:users" class="size-4 text-primary" />
              <span class="text-xs font-semibold text-foreground">
                {{ $t('record.face.detail.candidatesTitle') }}
              </span>
            </div>
            <span class="text-[11px] text-muted-foreground">
              Top-K 空间特征向量欧氏距离匹配
            </span>
          </div>

          <FaceCandidatesTable
            :candidates="currentDetail?.candidates"
            :top1-similarity="currentDetail?.similarity"
            :empty-text="$t('record.face.detail.noCandidates')"
          />
        </div>
      </div>
    </DetailModal>
  </Page>
</template>

<style scoped>
:deep([data-slot="form-label"]) {
  display: inline-block !important;
  text-align: justify !important;
  text-align-last: justify !important;
  width: 76px !important;
  min-width: 76px !important;
  padding-right: 0 !important;
  margin-right: 8px !important;
  line-height: 32px !important;
  font-weight: 500;
}

.candidate-table th,
.candidate-table td {
  text-align: center !important;
  vertical-align: middle !important;
}

.meta-label {
  display: inline-block;
  width: 78px;
  min-width: 78px;
  text-align: justify;
  text-align-last: justify;
  color: hsl(var(--muted-foreground));
  font-size: 12px;
  font-weight: 500;
  line-height: 1.5;
}

.meta-colon {
  display: inline-block;
  color: hsl(var(--muted-foreground));
  font-size: 12px;
  font-weight: 400;
  margin-left: 2px;
  margin-right: 6px;
}
</style>
