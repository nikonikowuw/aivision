<script lang="ts" setup>
import type { VxeTableGridOptions } from '#/adapter/vxe-table';
import type { FaceCaptureApi } from '#/api';

import { ref } from 'vue';

import { Page } from '@vben/common-ui';
import { $t } from '@vben/locales';

import { Button, Space, Tag } from 'ant-design-vue';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  getCameraPageApi,
  getFaceCaptureDetailApi,
  getFaceCaptureListApi,
} from '#/api';
import { getTodayRange } from '#/utils/date';

import CaptureFilmstripDrawer from './components/CaptureFilmstripDrawer.vue';
import CaptureThumbnail from './components/CaptureThumbnail.vue';

const currentCapture = ref<FaceCaptureApi.FaceCaptureItem | null>(null);
const filmstripDrawerOpen = ref<boolean>(false);

const gridOptions: VxeTableGridOptions<FaceCaptureApi.FaceCaptureItem> = {
  rowConfig: {
    keyField: 'id',
    isHover: true,
  },
  columns: [
    { field: 'id', title: $t('record.capture.columns.id'), width: 70 },
    {
      field: 'bestFaceCrop',
      title: $t('record.capture.columns.bestFaceCrop'),
      width: 96,
      align: 'center',
      slots: { default: 'bestFaceCrop' },
    },
    {
      field: 'bestPanorama',
      title: $t('record.capture.columns.bestPanorama'),
      width: 90,
      align: 'center',
      slots: { default: 'bestPanorama' },
    },
    {
      field: 'snapshotCount',
      title: $t('record.capture.columns.snapshotCount'),
      width: 100,
      align: 'center',
      slots: { default: 'snapshotCount' },
    },
    {
      field: 'personName',
      title: $t('record.capture.columns.personName'),
      minWidth: 130,
      slots: { default: 'personName' },
    },
    {
      field: 'bestSimilarity',
      title: $t('record.capture.columns.bestSimilarity'),
      width: 110,
      align: 'center',
      slots: { default: 'bestSimilarity' },
    },
    {
      field: 'cameraName',
      title: $t('record.capture.columns.cameraName'),
      minWidth: 140,
    },
    {
      field: 'firstObservedAt',
      title: $t('record.capture.columns.capturedAt'),
      minWidth: 170,
      formatter: 'formatDateTime',
    },
    {
      field: 'trackId',
      title: $t('record.capture.columns.trackId'),
      width: 90,
      align: 'center',
      formatter: ({ cellValue }) => (cellValue ? `#${cellValue}` : '-'),
    },
    {
      field: 'actions',
      fixed: 'right',
      showOverflow: false,
      slots: { default: 'actions' },
      title: $t('system.common.action'),
      width: 150,
    },
  ],
  pagerConfig: {
    enabled: true,
  },
  proxyConfig: {
    ajax: {
      query: async ({ page }, formValues) => {
        const {
          maxSimilarity,
          minSimilarity,
          strangerType,
          timeRange,
          ...rest
        } = formValues || {};

        let startTime: string | undefined;
        let endTime: string | undefined;
        if (timeRange && Array.isArray(timeRange) && timeRange.length === 2) {
          startTime = timeRange[0];
          endTime = timeRange[1];
        }

        let status: string | undefined;
        if (strangerType && strangerType !== 'all') {
          status = strangerType;
        }

        return await getFaceCaptureListApi({
          endTime,
          status,
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

const [Grid, gridApi] = useVbenVxeGrid({
  formOptions: {
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
          placeholder: $t('record.capture.filter.cameraPlaceholder'),
        },
        fieldName: 'cameraId',
        label: $t('record.capture.filter.camera'),
      },
      {
        component: 'Select',
        componentProps: {
          allowClear: true,
          options: [
            { label: $t('record.capture.filter.strangerAll'), value: 'all' },
            {
              label: $t('record.capture.filter.strangerTrue'),
              value: 'stranger',
            },
            {
              label: $t('record.capture.filter.strangerFalse'),
              value: 'recognized',
            },
          ],
          placeholder: $t('record.capture.filter.stranger'),
        },
        fieldName: 'strangerType',
        label: $t('record.capture.filter.stranger'),
      },
      {
        component: 'InputNumber',
        componentProps: {
          placeholder: $t('record.capture.filter.trackIdPlaceholder'),
          min: 1,
        },
        fieldName: 'trackId',
        label: $t('record.capture.filter.trackId'),
      },
      {
        component: 'Input',
        componentProps: {
          placeholder: $t('record.capture.filter.personNamePlaceholder'),
        },
        fieldName: 'personName',
        label: $t('record.capture.filter.personName'),
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
        },
        fieldName: 'minSimilarity',
        label: $t('record.capture.filter.minSimilarity'),
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
        label: $t('record.capture.filter.maxSimilarity'),
      },
    ],
  },
  gridOptions,
});

function toRatio(value: unknown): number | undefined {
  if (typeof value !== 'number' || !Number.isFinite(value)) return undefined;
  return value / 100;
}

async function handleViewFilmstrip(row: FaceCaptureApi.FaceCaptureItem) {
  try {
    currentCapture.value = row;
    filmstripDrawerOpen.value = true;
    const full = await getFaceCaptureDetailApi(row.id);
    if (full) {
      currentCapture.value = full;
    }
  } catch (error) {
    console.error('Failed to load face capture detail:', error);
  }
}

function handleRegisterSuccess() {
  gridApi.reload();
}

function formatSimilarity(value?: number): string {
  return typeof value === 'number' && value > 0
    ? `${(value * 100).toFixed(1)}%`
    : '-';
}
</script>

<template>
  <Page auto-content-height>
    <Grid>
      <template #bestFaceCrop="{ row }">
        <CaptureThumbnail
          fit="cover"
          :height="56"
          :url="row.bestFaceCropUrl || row.faceImageUrl"
          :width="56"
          :alt="$t('record.capture.columns.bestFaceCrop')"
        />
      </template>

      <template #bestPanorama="{ row }">
        <CaptureThumbnail
          fit="cover"
          :height="48"
          :url="row.bestPanoramaUrl || row.panoramaImageUrl"
          :bbox="row.bestBbox"
          :width="72"
          :alt="$t('record.capture.columns.bestPanorama')"
        />
      </template>

      <template #snapshotCount="{ row }">
        <Tag color="cyan"> 🎞️ {{ row.snapshotCount }} 帧 </Tag>
      </template>

      <template #personName="{ row }">
        <Tag v-if="row.isStranger || !row.bestPersonId" color="default">
          {{ $t('record.capture.columns.stranger') }}
        </Tag>
        <span v-else class="font-medium text-primary">
          {{ row.bestPersonName || '-' }}
        </span>
      </template>

      <template #bestSimilarity="{ row }">
        <Tag v-if="row.bestSimilarity >= 0.8" color="green">
          {{ formatSimilarity(row.bestSimilarity) }}
        </Tag>
        <Tag v-else-if="row.bestSimilarity > 0" color="orange">
          {{ formatSimilarity(row.bestSimilarity) }}
        </Tag>
        <span v-else class="text-neutral-400">-</span>
      </template>

      <template #actions="{ row }">
        <Space size="small">
          <Button size="small" type="link" @click="handleViewFilmstrip(row)">
            {{ $t('record.capture.actions.viewFilmstrip') }}
          </Button>
        </Space>
      </template>
    </Grid>

    <CaptureFilmstripDrawer
      v-model:open="filmstripDrawerOpen"
      :capture="currentCapture"
      @register-success="handleRegisterSuccess"
    />
  </Page>
</template>
