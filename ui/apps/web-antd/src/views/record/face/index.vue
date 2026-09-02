<script lang="ts" setup>
import type { VxeTableGridOptions } from '#/adapter/vxe-table';
import type { FaceObservationApi } from '#/api';

import { ref } from 'vue';

import { Page, useVbenModal } from '@vben/common-ui';
import { $t } from '@vben/locales';

import {
  Button,
  Card,
  Descriptions,
  DescriptionsItem,
  Tag,
  Tooltip,
} from 'ant-design-vue';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  getCameraPageApi,
  getFaceObservationDetailApi,
  getFaceObservationListApi,
} from '#/api';

import CaptureThumbnail from '../capture/components/CaptureThumbnail.vue';

const currentDetail = ref<FaceObservationApi.FaceObservationItem | null>(null);

const [DetailModal, detailModalApi] = useVbenModal({
  class: 'w-[1000px] max-w-[95vw]',
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
      { field: 'id', title: $t('record.face.columns.id'), width: 70 },
      {
        field: 'faceCrop',
        title: $t('record.face.columns.faceCrop'),
        width: 96,
        align: 'center',
        slots: { default: 'faceCrop' },
      },
      {
        field: 'panorama',
        title: $t('record.face.columns.panorama'),
        width: 90,
        align: 'center',
        slots: { default: 'panorama' },
      },
      {
        field: 'personName',
        title: $t('record.face.columns.personName'),
        minWidth: 130,
        slots: { default: 'personName' },
      },
      {
        field: 'personId',
        title: $t('record.face.columns.personId'),
        minWidth: 150,
        slots: { default: 'personId' },
      },
      {
        field: 'similarity',
        title: $t('record.face.columns.similarity'),
        width: 110,
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
      },
      {
        field: 'trackId',
        title: $t('record.face.columns.trackId'),
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
        width: 90,
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

function formatSimilarity(value?: number): string {
  return typeof value === 'number' ? `${(value * 100).toFixed(1)}%` : '-';
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
          :alt="$t('record.face.detail.faceCrop')"
        />
      </template>

      <template #panorama="{ row }">
        <CaptureThumbnail
          fit="cover"
          :height="48"
          :url="row.panoramaImageUrl"
          :bbox="row.bbox"
          :width="72"
          :alt="$t('record.face.detail.panorama')"
        />
      </template>

      <template #personName="{ row }">
        <span class="font-medium">{{ row.personName || '-' }}</span>
      </template>

      <template #personId="{ row }">
        <Tooltip v-if="row.personId" :title="row.personId">
          <span class="block max-w-[150px] truncate font-mono text-xs">
            {{ row.personId }}
          </span>
        </Tooltip>
        <span v-else>-</span>
      </template>

      <template #similarity="{ row }">
        <Tag :color="row.similarity >= 0.85 ? 'green' : 'blue'">
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
      <div v-if="currentDetail" class="space-y-6">
        <Descriptions bordered :column="{ xs: 1, sm: 2, md: 3 }" size="small">
          <DescriptionsItem :label="$t('record.face.detail.personName')">
            {{ currentDetail.personName || '-' }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.face.detail.personId')">
            {{ currentDetail.personId || '-' }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.face.detail.faceId')">
            <Tooltip v-if="currentDetail.faceId" :title="currentDetail.faceId">
              <span class="max-w-[180px] truncate font-mono text-xs">
                {{ currentDetail.faceId }}
              </span>
            </Tooltip>
            <span v-else>-</span>
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.face.detail.similarity')">
            <Tag color="green">
              {{ formatSimilarity(currentDetail.similarity) }}
            </Tag>
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.face.detail.algorithm')">
            {{ currentDetail.algorithmId || '-' }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.face.detail.algorithmVersion')">
            {{ currentDetail.algorithmVersion || '-' }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.face.detail.trackId')">
            {{ currentDetail.trackId ? `#${currentDetail.trackId}` : '-' }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.face.detail.timeSynced')">
            <Tag :color="currentDetail.timeSynced ? 'green' : 'orange'">
              {{
                currentDetail.timeSynced
                  ? $t('record.face.detail.synced')
                  : $t('record.face.detail.notSynced')
              }}
            </Tag>
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.face.detail.camera')">
            {{ currentDetail.cameraName || currentDetail.cameraId || '-' }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.face.detail.observedAt')">
            {{ currentDetail.observedAt }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.face.detail.eventId')" :span="2">
            <Tooltip :title="currentDetail.eventId">
              <span class="max-w-[280px] truncate font-mono text-xs">
                {{ currentDetail.eventId }}
              </span>
            </Tooltip>
          </DescriptionsItem>
        </Descriptions>

        <div class="grid grid-cols-1 gap-6 md:grid-cols-3">
          <Card
            class="md:col-span-1"
            size="small"
            :title="$t('record.face.detail.faceCrop')"
          >
            <div
              class="flex h-[240px] items-center justify-center rounded bg-neutral-900 p-4"
            >
              <CaptureThumbnail
                fit="contain"
                :height="200"
                :url="currentDetail.faceImageUrl"
                :width="200"
                :alt="$t('record.face.detail.faceCrop')"
              />
            </div>
          </Card>

          <Card
            class="md:col-span-2"
            size="small"
            :title="$t('record.face.detail.panorama')"
          >
            <div
              class="flex h-[240px] items-center justify-center rounded bg-neutral-900 p-2"
            >
              <CaptureThumbnail
                fit="contain"
                :height="220"
                :url="currentDetail.panoramaImageUrl"
                :bbox="currentDetail.bbox"
                :width="440"
                :alt="$t('record.face.detail.panorama')"
              />
            </div>
          </Card>
        </div>
      </div>
    </DetailModal>
  </Page>
</template>
