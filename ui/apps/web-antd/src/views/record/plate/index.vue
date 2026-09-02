<script lang="ts" setup>
import type { VxeTableGridOptions } from '#/adapter/vxe-table';
import type { PlateObservationApi } from '#/api';

import { ref } from 'vue';

import { Page, useVbenModal } from '@vben/common-ui';
import { $t } from '@vben/locales';
import { formatDateTime } from '@vben/utils';

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
  getPlateObservationDetailApi,
  getPlateObservationListApi,
} from '#/api';
import { getTodayRange } from '#/utils/date';

import PlateTag from './components/PlateTag.vue';
import PlateThumbnail from './components/PlateThumbnail.vue';

const currentDetail = ref<null | PlateObservationApi.PlateObservationItem>(
  null,
);

const [DetailModal, detailModalApi] = useVbenModal({
  class: 'w-[1000px] max-w-[95vw]',
  fullscreenButton: true,
  onConfirm: () => {
    detailModalApi.close();
  },
  showCancelButton: false,
  title: $t('record.plate.detail.title'),
});

const gridOptions: VxeTableGridOptions<PlateObservationApi.PlateObservationItem> =
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
        field: 'plateCrop',
        title: $t('record.plate.columns.plateCrop'),
        width: 120,
        align: 'center',
        slots: { default: 'plateCrop' },
      },
      {
        field: 'panorama',
        title: $t('record.plate.columns.panorama'),
        width: 90,
        align: 'center',
        slots: { default: 'panorama' },
      },
      {
        field: 'plateText',
        title: $t('record.plate.columns.plateText'),
        minWidth: 140,
        slots: { default: 'plateText' },
      },
      {
        field: 'plateColor',
        title: $t('record.plate.columns.plateColor'),
        width: 100,
        align: 'center',
        slots: { default: 'plateColor' },
      },
      {
        field: 'plateType',
        title: $t('record.plate.columns.plateType'),
        minWidth: 130,
        slots: { default: 'plateType' },
      },
      {
        field: 'confidence',
        title: $t('record.plate.columns.confidence'),
        width: 110,
        align: 'center',
        slots: { default: 'confidence' },
      },
      {
        field: 'ocrConfidence',
        title: $t('record.plate.columns.ocrConfidence'),
        width: 110,
        align: 'center',
        slots: { default: 'ocrConfidence' },
      },
      {
        field: 'cameraName',
        title: $t('record.plate.columns.cameraName'),
        minWidth: 140,
      },
      {
        field: 'observedAt',
        formatter: 'formatDateTime',
        title: $t('record.plate.columns.observedAt'),
        width: 170,
      },
      {
        field: 'trackId',
        title: $t('record.plate.columns.trackId'),
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
          const { timeRange, minConfidence, minOcrConfidence, ...rest } =
            formValues || {};
          let startTime: string | undefined;
          let endTime: string | undefined;
          if (timeRange && Array.isArray(timeRange) && timeRange.length === 2) {
            startTime = timeRange[0];
            endTime = timeRange[1];
          }

          return await getPlateObservationListApi({
            endTime,
            minConfidence:
              minConfidence !== undefined && minConfidence !== ''
                ? Number(minConfidence) / 100
                : undefined,
            minOcrConfidence:
              minOcrConfidence !== undefined && minOcrConfidence !== ''
                ? Number(minOcrConfidence) / 100
                : undefined,
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
            return res.items.map((c) => ({
              label: c.name,
              value: c.cameraId,
            }));
          },
          placeholder: $t('record.plate.filter.cameraPlaceholder'),
        },
        fieldName: 'cameraId',
        label: $t('record.plate.filter.camera'),
      },
      {
        component: 'Input',
        componentProps: {
          placeholder: $t('record.plate.filter.plateTextPlaceholder'),
        },
        fieldName: 'plateText',
        label: $t('record.plate.filter.plateText'),
      },
      {
        component: 'Select',
        componentProps: {
          allowClear: true,
          options: [
            { label: $t('record.plate.colors.blue'), value: 'blue' },
            { label: $t('record.plate.colors.yellow'), value: 'yellow' },
            { label: $t('record.plate.colors.green'), value: 'green' },
            { label: $t('record.plate.colors.white'), value: 'white' },
            { label: $t('record.plate.colors.black'), value: 'black' },
          ],
          placeholder: $t('record.plate.filter.plateColorPlaceholder'),
        },
        fieldName: 'plateColor',
        label: $t('record.plate.filter.plateColor'),
      },
      {
        component: 'Select',
        componentProps: {
          allowClear: true,
          options: [
            { label: $t('record.plate.types.standard'), value: 'standard' },
            {
              label: $t('record.plate.types.double_yellow'),
              value: 'double_yellow',
            },
            { label: $t('record.plate.types.new_energy'), value: 'new_energy' },
            { label: $t('record.plate.types.police'), value: 'police' },
            { label: $t('record.plate.types.coach'), value: 'coach' },
            { label: $t('record.plate.types.embassy'), value: 'embassy' },
            { label: $t('record.plate.types.hk_macau'), value: 'hk_macau' },
            { label: $t('record.plate.types.other'), value: 'other' },
          ],
          placeholder: $t('record.plate.filter.plateTypePlaceholder'),
        },
        fieldName: 'plateType',
        label: $t('record.plate.filter.plateType'),
      },
      {
        component: 'RangePicker',
        componentProps: {
          showTime: true,
          valueFormat: 'YYYY-MM-DDTHH:mm:ssZ',
        },
        defaultValue: getTodayRange(),
        fieldName: 'timeRange',
        label: $t('record.plate.filter.timeRange'),
      },
      {
        component: 'InputNumber',
        componentProps: {
          addonAfter: '%',
          max: 100,
          min: 0,
        },
        fieldName: 'minConfidence',
        label: $t('record.plate.filter.minConfidence'),
      },
      {
        component: 'InputNumber',
        componentProps: {
          addonAfter: '%',
          max: 100,
          min: 0,
        },
        fieldName: 'minOcrConfidence',
        label: $t('record.plate.filter.minOcrConfidence'),
      },
    ],
  },
  gridOptions,
});

async function handleViewDetail(row: PlateObservationApi.PlateObservationItem) {
  try {
    detailModalApi.open();
    currentDetail.value = row;
    const full = await getPlateObservationDetailApi(row.id);
    if (full) {
      currentDetail.value = full;
    }
  } catch (error) {
    console.error('Failed to load observation detail:', error);
  }
}

function getPlateTypeLabel(type?: string): string {
  if (!type) return '-';
  const map: Record<string, string> = {
    coach: $t('record.plate.types.coach'),
    double_yellow: $t('record.plate.types.double_yellow'),
    embassy: $t('record.plate.types.embassy'),
    hk_macau: $t('record.plate.types.hk_macau'),
    new_energy: $t('record.plate.types.new_energy'),
    other: $t('record.plate.types.other'),
    police: $t('record.plate.types.police'),
    standard: $t('record.plate.types.standard'),
  };
  return map[type] || type;
}

function getPlateColorLabel(color?: string): string {
  if (!color) return '-';
  const map: Record<string, string> = {
    black: $t('record.plate.colors.black'),
    blue: $t('record.plate.colors.blue'),
    green: $t('record.plate.colors.green'),
    white: $t('record.plate.colors.white'),
    yellow: $t('record.plate.colors.yellow'),
  };
  return map[color] || color;
}
</script>

<template>
  <Page auto-content-height>
    <Grid>
      <template #plateCrop="{ row }">
        <PlateThumbnail
          fit="contain"
          :height="36"
          :url="row.plateImageUrl"
          :width="112"
        />
      </template>

      <template #panorama="{ row }">
        <PlateThumbnail
          fit="cover"
          :height="48"
          :url="row.panoramaImageUrl"
          :width="72"
        />
      </template>

      <template #plateText="{ row }">
        <PlateTag
          :color="row.plateColor"
          :text="row.plateText"
          :type="row.plateType"
        />
      </template>

      <template #plateColor="{ row }">
        <Tag
          :color="
            row.plateColor === 'green'
              ? 'success'
              : row.plateColor === 'yellow'
                ? 'warning'
                : row.plateColor === 'blue'
                  ? 'processing'
                  : 'default'
          "
        >
          {{ getPlateColorLabel(row.plateColor) }}
        </Tag>
      </template>

      <template #plateType="{ row }">
        <span>{{ getPlateTypeLabel(row.plateType) }}</span>
      </template>

      <template #confidence="{ row }">
        <Tag :color="row.confidence >= 0.8 ? 'green' : 'orange'">
          {{ (row.confidence * 100).toFixed(1) }}%
        </Tag>
      </template>

      <template #ocrConfidence="{ row }">
        <Tag :color="row.ocrConfidence >= 0.8 ? 'green' : 'orange'">
          {{ (row.ocrConfidence * 100).toFixed(1) }}%
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
          <DescriptionsItem :label="$t('record.plate.detail.plateText')">
            <PlateTag
              :color="currentDetail.plateColor"
              :text="currentDetail.plateText"
              :type="currentDetail.plateType"
            />
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.plate.detail.normalizedText')">
            {{ currentDetail.normalizedText || '-' }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.plate.detail.algorithm')">
            {{ currentDetail.algorithmId || '-' }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.plate.detail.algorithmVersion')">
            {{ currentDetail.algorithmVersion || '-' }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.plate.detail.plateColor')">
            {{ getPlateColorLabel(currentDetail.plateColor) }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.plate.detail.plateType')">
            {{ getPlateTypeLabel(currentDetail.plateType) }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.plate.detail.confidence')">
            <Tag color="green">
              {{ (currentDetail.confidence * 100).toFixed(1) }}%
            </Tag>
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.plate.detail.ocrConfidence')">
            <Tag color="cyan">
              {{ (currentDetail.ocrConfidence * 100).toFixed(1) }}%
            </Tag>
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.plate.detail.trackId')">
            {{ currentDetail.trackId ? `#${currentDetail.trackId}` : '-' }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.plate.detail.timeSynced')">
            <Tag :color="currentDetail.timeSynced ? 'green' : 'orange'">
              {{
                currentDetail.timeSynced
                  ? $t('record.plate.detail.synced')
                  : $t('record.plate.detail.notSynced')
              }}
            </Tag>
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.plate.detail.camera')">
            {{ currentDetail.cameraName || currentDetail.cameraId }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.plate.detail.observedAt')">
            {{ formatDateTime(currentDetail.observedAt) || '-' }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.plate.detail.eventId')">
            <Tooltip :title="currentDetail.eventId">
              <span class="max-w-[200px] truncate font-mono text-xs">{{
                currentDetail.eventId
              }}</span>
            </Tooltip>
          </DescriptionsItem>
        </Descriptions>

        <div class="grid grid-cols-1 gap-6 md:grid-cols-3">
          <Card
            class="md:col-span-1"
            size="small"
            :title="$t('record.plate.detail.plateCrop')"
          >
            <div
              class="flex h-[240px] items-center justify-center rounded bg-neutral-900 p-4"
            >
              <PlateThumbnail
                fit="contain"
                :height="80"
                :url="currentDetail.plateImageUrl"
                :width="240"
              />
            </div>
          </Card>

          <Card
            class="md:col-span-2"
            size="small"
            :title="$t('record.plate.detail.panorama')"
          >
            <div
              class="flex h-[240px] items-center justify-center rounded bg-neutral-900 p-2"
            >
              <PlateThumbnail
                fit="contain"
                :height="220"
                :url="currentDetail.panoramaImageUrl"
                :width="440"
              />
            </div>
          </Card>
        </div>
      </div>
    </DetailModal>
  </Page>
</template>
