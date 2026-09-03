<script lang="ts" setup>
import type { VxeTableGridOptions } from '#/adapter/vxe-table';
import type { CaptureApi } from '#/api';

import { ref } from 'vue';

import { Page } from '@vben/common-ui';
import { $t } from '@vben/locales';

import { Button, Tag } from 'ant-design-vue';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  getCameraPageApi,
  getCaptureDetailApi,
  getCaptureListApi,
} from '#/api';
import { getTodayRange } from '#/utils/date';
import { getConfidenceTagColor, getTargetTypeTagColor } from '#/utils/format';

import CaptureDetailDrawer from './components/CaptureDetailDrawer.vue';
import CaptureThumbnail from './components/CaptureThumbnail.vue';

const currentCapture = ref<CaptureApi.CaptureItem | null>(null);
const detailDrawerOpen = ref(false);

const targetOptions = [
  { label: $t('record.capture.targets.all'), value: 'all' },
  { label: $t('record.capture.targets.face'), value: 'face' },
  { label: $t('record.capture.targets.person'), value: 'person' },
  { label: $t('record.capture.targets.vehicle'), value: 'vehicle' },
  { label: $t('record.capture.targets.non_motor'), value: 'non_motor' },
  { label: $t('record.capture.targets.generic'), value: 'generic' },
];

function toRatio(val: unknown): number | undefined {
  if (typeof val === 'number') return val / 100;
  if (typeof val === 'string' && val.trim() !== '') {
    const num = Number(val);
    return Number.isFinite(num) ? num / 100 : undefined;
  }
  return undefined;
}

const gridOptions: VxeTableGridOptions<CaptureApi.CaptureItem> = {
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
      field: 'cropImageUrl',
      title: $t('record.capture.columns.crop'),
      width: 94,
      align: 'center',
      slots: { default: 'crop' },
    },
    {
      field: 'imageUrl',
      title: $t('record.capture.columns.panorama'),
      width: 104,
      align: 'center',
      slots: { default: 'panorama' },
    },
    {
      field: 'confidence',
      title: $t('record.capture.columns.confidence'),
      width: 112,
      align: 'center',
      slots: { default: 'confidence' },
    },
    {
      field: 'cameraName',
      title: $t('record.capture.columns.cameraName'),
      minWidth: 142,
    },
    {
      field: 'trackId',
      title: $t('record.capture.columns.trackId'),
      width: 88,
      align: 'center',
      formatter: ({ cellValue }) => (cellValue ? `#${cellValue}` : '-'),
    },
    {
      field: 'targetType',
      title: $t('record.capture.columns.targetType'),
      width: 104,
      align: 'center',
      slots: { default: 'targetType' },
    },
    {
      field: 'capturedAt',
      title: $t('record.capture.columns.capturedAt'),
      minWidth: 172,
      formatter: 'formatDateTime',
    },
    {
      field: 'actions',
      fixed: 'right',
      showOverflow: false,
      slots: { default: 'actions' },
      title: $t('system.common.action'),
      width: 106,
    },
  ],
  pagerConfig: {
    enabled: true,
  },
  proxyConfig: {
    ajax: {
      query: async ({ page }, formValues) => {
        const {
          timeRange,
          targetType,
          keyword,
          isRecognized,
          minConfidence,
          maxConfidence,
          ...rest
        } = formValues || {};
        let startTime: string | undefined;
        let endTime: string | undefined;
        if (Array.isArray(timeRange) && timeRange.length === 2) {
          startTime = timeRange[0];
          endTime = timeRange[1];
        }
        return await getCaptureListApi({
          ...rest,
          endTime,
          isRecognized:
            isRecognized === undefined ||
            isRecognized === '' ||
            isRecognized === 'all'
              ? undefined
              : isRecognized === 'true',
          keyword:
            typeof keyword === 'string' && keyword.trim()
              ? keyword.trim()
              : undefined,
          maxConfidence: toRatio(maxConfidence),
          minConfidence: toRatio(minConfidence),
          page: page.currentPage,
          pageSize: page.pageSize,
          startTime,
          targetType:
            targetType === 'all' || !targetType ? undefined : targetType,
        });
      },
    },
  },
};

const [Grid, gridApi] = useVbenVxeGrid({
  formOptions: {
    commonConfig: {
      labelWidth: 80,
    },
    schema: [
      {
        component: 'Select',
        componentProps: {
          allowClear: true,
          options: targetOptions,
          placeholder: $t('record.capture.filter.targetTypePlaceholder'),
        },
        fieldName: 'targetType',
        label: $t('record.capture.filter.targetType'),
      },
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
          placeholder: $t('record.capture.filter.cameraPlaceholder'),
        },
        fieldName: 'cameraId',
        label: $t('record.capture.filter.camera'),
      },
      {
        component: 'Input',
        componentProps: {
          allowClear: true,
          placeholder: $t('record.capture.filter.keywordPlaceholder'),
        },
        fieldName: 'keyword',
        label: $t('record.capture.filter.keyword'),
      },
      {
        component: 'RangePicker',
        componentProps: {
          showTime: true,
          valueFormat: 'YYYY-MM-DDTHH:mm:ssZ',
        },
        defaultValue: getTodayRange(),
        fieldName: 'timeRange',
        label: $t('record.capture.filter.timeRange'),
      },
      {
        component: 'InputNumber',
        componentProps: {
          addonAfter: '%',
          max: 100,
          min: 0,
          precision: 1,
          placeholder: $t('record.capture.advanced.min'),
        },
        fieldName: 'minConfidence',
        label: $t('record.capture.filter.minConfidence'),
      },
      {
        component: 'InputNumber',
        componentProps: {
          addonAfter: '%',
          max: 100,
          min: 0,
          precision: 1,
          placeholder: $t('record.capture.advanced.max'),
        },
        fieldName: 'maxConfidence',
        label: $t('record.capture.filter.maxConfidence'),
      },
      {
        component: 'Select',
        componentProps: {
          allowClear: true,
          options: [
            {
              label: $t('record.capture.advanced.recognizedYes'),
              value: 'true',
            },
            {
              label: $t('record.capture.advanced.recognizedNo'),
              value: 'false',
            },
          ],
          placeholder: $t('record.capture.advanced.recognizedPlaceholder'),
        },
        fieldName: 'isRecognized',
        label: $t('record.capture.advanced.recognized'),
      },
    ],
  },
  gridOptions,
});

function reloadGrid() {
  gridApi.reload();
}

async function handleViewCapture(row: CaptureApi.CaptureItem) {
  currentCapture.value = row;
  detailDrawerOpen.value = true;
  try {
    const full = await getCaptureDetailApi(row.id);
    if (full) currentCapture.value = full;
  } catch (error) {
    console.error('Failed to load capture detail:', error);
  }
}

function formatRatio(value: number): string {
  return `${(value * 100).toFixed(1)}%`;
}
</script>

<template>
  <Page auto-content-height>
    <Grid>
      <template #crop="{ row }">
        <CaptureThumbnail
          :alt="$t('record.capture.columns.crop')"
          fit="cover"
          :height="58"
          :url="row.cropImageUrl || row.subCropImageUrl"
          :width="72"
        />
      </template>

      <template #panorama="{ row }">
        <CaptureThumbnail
          :alt="$t('record.capture.columns.panorama')"
          fit="contain"
          :height="48"
          :url="row.imageUrl"
          :bbox="row.bbox"
          :width="85"
          class="mx-auto aspect-video"
        />
      </template>

      <template #confidence="{ row }">
        <Tag :color="getConfidenceTagColor(row.confidence)">
          {{ formatRatio(row.confidence) }}
        </Tag>
      </template>

      <template #targetType="{ row }">
        <Tag :color="getTargetTypeTagColor(row.targetType)">
          {{ $t(`record.capture.targets.${row.targetType}`) }}
        </Tag>
      </template>

      <template #actions="{ row }">
        <Button size="small" type="link" @click="handleViewCapture(row)">
          {{ $t('record.capture.actions.viewDetail') }}
        </Button>
      </template>
    </Grid>

    <CaptureDetailDrawer
      v-model:open="detailDrawerOpen"
      :capture="currentCapture"
      @register-success="reloadGrid"
    />
  </Page>
</template>

<style scoped>
:deep([data-slot='form-label']) {
  display: inline-block !important;
  width: 76px !important;
  min-width: 76px !important;
  padding-right: 0 !important;
  margin-right: 8px !important;
  font-weight: 500;
  line-height: 32px !important;
  text-align: justify !important;
  text-align-last: justify !important;
}
</style>
