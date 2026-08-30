<script lang="ts" setup>
import type { VxeTableGridOptions } from '#/adapter/vxe-table';
import type { AlarmRecordApi } from '#/api';

import { ref } from 'vue';

import { Page, useVbenModal } from '@vben/common-ui';
import { $t } from '@vben/locales';

import { Button, Descriptions, DescriptionsItem, Tag } from 'ant-design-vue';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  getAlarmRecordDetailApi,
  getAlarmRecordListApi,
  getAlgorithmList,
  getCameraPageApi,
} from '#/api';

import AlarmAnnotationCanvas from './components/AlarmAnnotationCanvas.vue';
import AlarmThumbnail from './components/AlarmThumbnail.vue';
import TargetCropCanvas from './components/TargetCropCanvas.vue';

const currentDetail = ref<AlarmRecordApi.AlarmRecordDetail | null>(null);

const [DetailModal, detailModalApi] = useVbenModal({
  showCancelButton: false,
  title: $t('record.alarm.detail.title'),
});

const gridOptions: VxeTableGridOptions<AlarmRecordApi.AlarmRecordItem> = {
  rowConfig: {
    keyField: 'id',
    isHover: true,
  },
  columns: [
    { field: 'id', title: $t('record.alarm.columns.id'), width: 70 },
    {
      field: 'targetCrop',
      title: $t('record.alarm.columns.targetCrop'),
      width: 90,
      align: 'center',
      slots: { default: 'targetCrop' },
    },
    {
      field: 'panorama',
      title: $t('record.alarm.columns.panorama'),
      width: 100,
      align: 'center',
      slots: { default: 'panorama' },
    },
    {
      field: 'occurredAt',
      formatter: 'formatDateTime',
      title: $t('record.alarm.columns.occurredAt'),
      width: 170,
    },
    {
      field: 'cameraName',
      title: $t('record.alarm.columns.cameraName'),
      minWidth: 130,
      slots: { default: 'cameraName' },
    },
    {
      field: 'algorithmName',
      title: $t('record.alarm.columns.algorithmName'),
      minWidth: 130,
      slots: { default: 'algorithmName' },
    },
    {
      field: 'alarmTypeId',
      title: $t('record.alarm.columns.alarmTypeId'),
      minWidth: 130,
      slots: { default: 'alarmTypeId' },
    },
    {
      field: 'targetLabel',
      title: $t('record.alarm.columns.targetLabel'),
      width: 100,
      slots: { default: 'targetLabel' },
    },
    {
      field: 'trackId',
      title: $t('record.alarm.columns.trackId'),
      width: 90,
      align: 'center',
      formatter: ({ cellValue }) => (cellValue ? `#${cellValue}` : '-'),
    },
    {
      field: 'confidence',
      title: $t('record.alarm.columns.confidence'),
      width: 110,
      align: 'center',
      slots: { default: 'confidence' },
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
  // 保持与其他管理页面（camera/user/log/task）一致的标准自适应表格高度，不强制启用会截断视口高度的 scrollY 虚拟滚动
  proxyConfig: {
    ajax: {
      query: async ({ page }, formValues) => {
        const { timeRange, minConfidence, maxConfidence, ...rest } =
          formValues || {};
        let startTime: string | undefined;
        let endTime: string | undefined;
        if (timeRange && Array.isArray(timeRange) && timeRange.length === 2) {
          startTime = timeRange[0];
          endTime = timeRange[1];
        }

        return await getAlarmRecordListApi({
          endTime,
          maxConfidence:
            maxConfidence !== undefined && maxConfidence !== ''
              ? Number(maxConfidence) / 100
              : undefined,
          minConfidence:
            minConfidence !== undefined && minConfidence !== ''
              ? Number(minConfidence) / 100
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
          placeholder: $t('record.alarm.filter.cameraPlaceholder'),
        },
        fieldName: 'cameraId',
        label: $t('record.alarm.filter.camera'),
      },
      {
        component: 'ApiSelect',
        componentProps: {
          allowClear: true,
          api: async () => {
            const res = await getAlgorithmList({ page: 1, pageSize: 100 });
            return res.items.map((a) => ({
              label: a.name,
              value: a.algorithmId,
            }));
          },
          placeholder: $t('record.alarm.filter.algorithmPlaceholder'),
        },
        fieldName: 'algorithmId',
        label: $t('record.alarm.filter.algorithm'),
      },
      {
        component: 'Input',
        fieldName: 'alarmTypeId',
        label: $t('record.alarm.filter.alarmType'),
      },
      {
        component: 'RangePicker',
        componentProps: {
          showTime: true,
          valueFormat: 'YYYY-MM-DDTHH:mm:ssZ',
        },
        fieldName: 'timeRange',
        label: $t('record.alarm.filter.timeRange'),
      },
      {
        component: 'InputNumber',
        componentProps: {
          addonAfter: '%',
          max: 100,
          min: 0,
        },
        fieldName: 'minConfidence',
        label: $t('record.alarm.filter.minConfidence'),
      },
      {
        component: 'InputNumber',
        componentProps: {
          addonAfter: '%',
          max: 100,
          min: 0,
        },
        fieldName: 'maxConfidence',
        label: $t('record.alarm.filter.maxConfidence'),
      },
    ],
  },
  gridOptions,
});

async function handleViewDetail(row: AlarmRecordApi.AlarmRecordItem) {
  try {
    currentDetail.value = null; // 每次打开前先重置当前详情，避免旧数据残影
    const detail = await getAlarmRecordDetailApi(row.id);
    currentDetail.value = detail || (row as AlarmRecordApi.AlarmRecordDetail);
    detailModalApi.open();
  } catch (error) {
    console.error(error);
  }
}
</script>

<template>
  <Page auto-content-height>
    <Grid>
      <!-- 目标图插槽 -->
      <template #targetCrop="{ row }">
        <TargetCropCanvas
          v-if="row.imageId && row.bbox && row.bbox.length === 4"
          :image-id="row.imageId"
          :bbox="row.bbox"
          :width="54"
          :height="54"
          :pad-ratio="0.2"
        />
        <span v-else class="text-xs text-muted-foreground">-</span>
      </template>

      <!-- 全景图缩略图插槽 -->
      <template #panorama="{ row }">
        <AlarmThumbnail
          v-if="row.imageId"
          :image-id="row.imageId"
          :bbox="row.bbox"
          :width="64"
          :height="48"
        />
        <span v-else class="text-xs text-muted-foreground">-</span>
      </template>

      <template #cameraName="{ row }">
        <span>{{ row.cameraName || row.cameraId }}</span>
      </template>

      <template #algorithmName="{ row }">
        <span>{{ row.algorithmName || row.algorithmId }}</span>
      </template>

      <template #alarmTypeId="{ row }">
        <Tag color="blue">{{ row.alarmTypeId }}</Tag>
      </template>

      <template #targetLabel="{ row }">
        <span>{{ row.targetLabel || 'Target' }}</span>
      </template>

      <template #confidence="{ row }">
        <span class="font-semibold text-red-500">
          {{ (row.confidence * 100).toFixed(1) }}%
        </span>
      </template>

      <template #actions="{ row }">
        <Button
          type="link"
          size="small"
          v-access:code="['record:alarm']"
          @click="() => handleViewDetail(row)"
        >
          {{ $t('system.common.detail') }}
        </Button>
      </template>
    </Grid>

    <DetailModal>
      <div v-if="currentDetail" :key="currentDetail.id" class="flex flex-col gap-4 p-4">
        <!-- 基本事件元数据 -->
        <Descriptions :column="2" bordered size="small">
          <DescriptionsItem :label="$t('record.alarm.detail.eventId')" :span="2">
            <span class="font-mono text-xs">{{ currentDetail.eventId }}</span>
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.alarm.detail.occurredAt')">
            {{ currentDetail.occurredAt }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.alarm.detail.timeSynced')">
            <Tag :color="currentDetail.timeSynced ? 'success' : 'warning'">
              {{ currentDetail.timeSynced ? 'Yes' : 'No' }}
            </Tag>
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.alarm.detail.camera')">
            {{ currentDetail.cameraName || currentDetail.cameraId }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.alarm.detail.algorithm')">
            {{ currentDetail.algorithmName || currentDetail.algorithmId }}
            <span class="text-muted-foreground"> (v{{ currentDetail.algorithmVersion }})</span>
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.alarm.detail.alarmType')">
            <Tag color="blue">{{ currentDetail.alarmTypeId }}</Tag>
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.alarm.detail.targetLabel')">
            <span>{{ currentDetail.targetLabel || 'Target' }}</span>
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.alarm.detail.trackId')">
            <span class="font-mono">{{ currentDetail.trackId ? `#${currentDetail.trackId}` : '-' }}</span>
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.alarm.detail.confidence')">
            <span class="font-bold text-red-500">
              {{ (currentDetail.confidence * 100).toFixed(1) }}%
            </span>
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.alarm.columns.targetCrop')" :span="2">
            <div class="flex items-center gap-3">
              <TargetCropCanvas
                :image-id="currentDetail.imageId"
                :bbox="currentDetail.bbox"
                :width="96"
                :height="96"
              />
              <span class="text-xs text-muted-foreground">
                (点击目标特写可查看原图 1:1 无损超清放大)
              </span>
            </div>
          </DescriptionsItem>
        </Descriptions>

        <!-- 全景底图与叠加标注 -->
        <div class="flex flex-col gap-2 rounded border border-border bg-card p-3">
          <span class="font-medium text-xs text-foreground">
            {{ $t('record.alarm.detail.imageAnnotation') }}
          </span>

          <!-- 图例 -->
          <div class="flex flex-wrap gap-4 text-xs">
            <span class="flex items-center gap-1">
              <span class="inline-block h-3 w-3 border border-yellow-400 bg-yellow-400/30"></span>
              {{ $t('record.alarm.detail.legendRoi') }}
            </span>
            <span class="flex items-center gap-1">
              <span class="inline-block h-3 w-3 border border-gray-400 bg-gray-600/40"></span>
              {{ $t('record.alarm.detail.legendMask') }}
            </span>
            <span class="flex items-center gap-1">
              <span class="inline-block h-1 w-3 bg-amber-500"></span>
              {{ $t('record.alarm.detail.legendLine') }}
            </span>
            <span class="flex items-center gap-1">
              <span class="inline-block h-3 w-3 border border-red-500 bg-red-500/20"></span>
              {{ $t('record.alarm.detail.legendTarget') }}
            </span>
          </div>

          <AlarmAnnotationCanvas :detail="currentDetail" />
        </div>
      </div>
    </DetailModal>
  </Page>
</template>
