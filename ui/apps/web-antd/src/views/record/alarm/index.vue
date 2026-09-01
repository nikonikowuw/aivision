<script lang="ts" setup>
import type { VxeTableGridOptions } from '#/adapter/vxe-table';
import type { AlarmRecordApi } from '#/api';

import { ref } from 'vue';

import { Page, useVbenModal } from '@vben/common-ui';
import { $t } from '@vben/locales';

import {
  Button,
  Descriptions,
  DescriptionsItem,
  Tag,
  Tooltip,
} from 'ant-design-vue';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  getAlarmRecordDetailApi,
  getAlarmRecordListApi,
  getAlgorithmList,
  getCameraPageApi,
} from '#/api';
import {
  formatAlarmTypeName,
  formatAlgorithmName,
  formatTargetClass,
} from '#/utils/i18n';

import AlarmAnnotationCanvas from './components/AlarmAnnotationCanvas.vue';
import AlarmThumbnail from './components/AlarmThumbnail.vue';
import TargetCropCanvas from './components/TargetCropCanvas.vue';

const currentDetail = ref<AlarmRecordApi.AlarmRecordDetail | null>(null);

const [DetailModal, detailModalApi] = useVbenModal({
  class: 'w-[1100px] max-w-[95vw]',
  fullscreenButton: true,
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
        <span>{{
          formatAlgorithmName(row.algorithmId, row.algorithmName)
        }}</span>
      </template>

      <template #alarmTypeId="{ row }">
        <Tag color="blue">{{ formatAlarmTypeName(row.alarmTypeId) }}</Tag>
      </template>

      <template #targetLabel="{ row }">
        <span>{{ formatTargetClass(row.targetLabel) }}</span>
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
      <div
        v-if="currentDetail"
        :key="currentDetail.id"
        class="flex flex-col gap-4 p-2 lg:flex-row"
      >
        <!-- 左侧：宽幅全景大图与智能图层（占主视觉宽度） -->
        <div class="flex flex-1 flex-col overflow-hidden">
          <AlarmAnnotationCanvas :detail="currentDetail" />
        </div>

        <!-- 右侧：结构化事件与目标侧边栏 (340px 紧凑面板) -->
        <div class="flex w-full flex-col gap-3.5 lg:w-[340px] lg:shrink-0">
          <!-- 识别目标特写高亮卡片 -->
          <div
            class="flex flex-col gap-3 rounded-lg border border-border/80 bg-card p-3.5 shadow-xs"
          >
            <div class="flex items-center justify-between">
              <span class="text-xs font-semibold text-foreground">
                {{ $t('record.alarm.detail.targetCard') }}
              </span>
              <Tag color="blue" class="m-0 font-medium">
                {{ formatAlarmTypeName(currentDetail.alarmTypeId) }}
              </Tag>
            </div>

            <div class="flex items-center gap-3.5">
              <!-- 高清目标特写（带无损放大） -->
              <Tooltip :title="$t('record.alarm.detail.cropTip')">
                <div class="shrink-0">
                  <TargetCropCanvas
                    :image-id="currentDetail.imageId"
                    :bbox="currentDetail.bbox"
                    :width="80"
                    :height="80"
                  />
                </div>
              </Tooltip>

              <div class="flex flex-1 flex-col gap-1.5 overflow-hidden">
                <div class="flex items-center gap-1.5 truncate">
                  <span class="text-sm font-bold text-foreground truncate">
                    {{ formatTargetClass(currentDetail.targetLabel) }}
                  </span>
                  <Tag
                    v-if="currentDetail.trackId"
                    color="purple"
                    class="m-0 text-[11px] font-mono px-1 py-0"
                  >
                    #{{ currentDetail.trackId }}
                  </Tag>
                </div>

                <div class="flex items-baseline gap-1.5">
                  <span class="text-xs text-muted-foreground">
                    {{ $t('record.alarm.detail.confidence') }}:
                  </span>
                  <span class="text-base font-bold text-red-500">
                    {{ (currentDetail.confidence * 100).toFixed(1) }}%
                  </span>
                </div>
              </div>
            </div>
          </div>

          <!-- 事件元数据详细表格 -->
          <div
            class="rounded-lg border border-border/80 bg-card p-3.5 shadow-xs"
          >
            <div class="mb-2.5 font-semibold text-xs text-foreground">
              {{ $t('record.alarm.detail.eventInfo') }}
            </div>

            <Descriptions
              :column="1"
              size="small"
              class="alarm-detail-desc"
              :label-style="{ width: '85px' }"
            >
              <DescriptionsItem :label="$t('record.alarm.detail.occurredAt')">
                <span class="text-xs text-foreground">{{
                  currentDetail.occurredAt
                }}</span>
              </DescriptionsItem>
              <DescriptionsItem :label="$t('record.alarm.detail.camera')">
                <span
                  class="text-xs font-medium text-foreground truncate"
                  :title="currentDetail.cameraName || currentDetail.cameraId"
                >
                  {{ currentDetail.cameraName || currentDetail.cameraId }}
                </span>
              </DescriptionsItem>
              <DescriptionsItem :label="$t('record.alarm.detail.algorithm')">
                <div class="flex flex-wrap items-center gap-1">
                  <span class="text-xs text-foreground">
                    {{
                      formatAlgorithmName(
                        currentDetail.algorithmId,
                        currentDetail.algorithmName,
                      )
                    }}
                  </span>
                  <Tag
                    v-if="currentDetail.algorithmVersion"
                    class="m-0 text-[10px] px-1 py-0"
                  >
                    v{{ currentDetail.algorithmVersion }}
                  </Tag>
                </div>
              </DescriptionsItem>
              <DescriptionsItem :label="$t('record.alarm.detail.timeSynced')">
                <Tag
                  :color="currentDetail.timeSynced ? 'success' : 'warning'"
                  class="m-0 text-[11px]"
                >
                  {{
                    currentDetail.timeSynced
                      ? $t('record.alarm.detail.synced')
                      : $t('record.alarm.detail.notSynced')
                  }}
                </Tag>
              </DescriptionsItem>
              <DescriptionsItem :label="$t('record.alarm.detail.eventId')">
                <span
                  class="font-mono text-[11px] text-muted-foreground break-all"
                >
                  {{ currentDetail.eventId }}
                </span>
              </DescriptionsItem>
            </Descriptions>
          </div>
        </div>
      </div>
    </DetailModal>
  </Page>
</template>

<style scoped>
:deep(.alarm-detail-desc .ant-descriptions-item-label) {
  padding-bottom: 6px;
  font-size: 12px;
  color: var(--ant-color-text-secondary, rgb(0 0 0 / 45%));
}

:deep(.alarm-detail-desc .ant-descriptions-item-content) {
  padding-bottom: 6px;
  font-size: 12px;
}
</style>
