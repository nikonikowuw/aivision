<script lang="ts" setup>
import type { VxeTableGridOptions } from '#/adapter/vxe-table';
import type { AlarmRecordApi } from '#/api';

import { ref } from 'vue';

import { Page, useVbenModal } from '@vben/common-ui';
import { $t } from '@vben/locales';
import { formatDateTime } from '@vben/utils';

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
  getCameraPageApi,
} from '#/api';
import { getTodayRange } from '#/utils/date';
import { getConfidenceTagColor } from '#/utils/format';
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
  onConfirm: () => {
    detailModalApi.close();
  },
  showCancelButton: false,
  title: $t('record.alarm.detail.title'),
});

const gridOptions: VxeTableGridOptions<AlarmRecordApi.AlarmRecordItem> = {
  rowConfig: {
    keyField: 'id',
    isHover: true,
  },
  columns: [
    {
      type: 'seq',
      title: $t('system.common.index'),
      width: 64,
      align: 'center',
    },
    {
      field: 'targetCrop',
      title: $t('record.alarm.columns.targetCrop'),
      width: 96,
      align: 'center',
      slots: { default: 'targetCrop' },
    },
    {
      field: 'panorama',
      title: $t('record.alarm.columns.panorama'),
      width: 112,
      align: 'center',
      slots: { default: 'panorama' },
    },
    {
      field: 'confidence',
      title: $t('record.alarm.columns.confidence'),
      width: 112,
      align: 'center',
      slots: { default: 'confidence' },
    },
    {
      field: 'cameraName',
      title: $t('record.alarm.columns.cameraName'),
      minWidth: 150,
      slots: { default: 'cameraName' },
    },
    {
      field: 'alarmTypeId',
      title: $t('record.alarm.columns.alarmTypeId'),
      minWidth: 160,
      align: 'center',
      slots: { default: 'alarmTypeId' },
    },
    {
      field: 'targetLabel',
      title: $t('record.alarm.columns.targetLabel'),
      minWidth: 150,
      align: 'center',
      slots: { default: 'targetLabel' },
    },
    {
      field: 'trackId',
      title: $t('record.alarm.columns.trackId'),
      width: 100,
      align: 'center',
      formatter: ({ cellValue }) => (cellValue ? `#${cellValue}` : '-'),
    },
    {
      field: 'occurredAt',
      formatter: 'formatDateTime',
      title: $t('record.alarm.columns.occurredAt'),
      width: 180,
      align: 'center',
    },
    {
      field: 'actions',
      fixed: 'right',
      showOverflow: false,
      slots: { default: 'actions' },
      title: $t('system.common.action'),
      width: 96,
      align: 'center',
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
          targetLabel,
          minConfidence,
          maxConfidence,
          ...rest
        } = formValues || {};
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
          targetLabel:
            typeof targetLabel === 'string' && targetLabel.trim()
              ? targetLabel.trim()
              : undefined,
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
        component: 'Input',
        fieldName: 'alarmTypeId',
        label: $t('record.alarm.filter.alarmType'),
      },
      {
        component: 'Input',
        componentProps: {
          allowClear: true,
          placeholder: $t('record.alarm.filter.targetTypePlaceholder'),
        },
        fieldName: 'targetLabel',
        label: $t('record.alarm.filter.targetType'),
      },
      {
        component: 'RangePicker',
        componentProps: {
          showTime: true,
          valueFormat: 'YYYY-MM-DDTHH:mm:ssZ',
        },
        defaultValue: getTodayRange(),
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
          :width="72"
          :height="41"
          class="mx-auto aspect-video"
        />
        <span v-else class="text-xs text-muted-foreground">-</span>
      </template>

      <template #confidence="{ row }">
        <Tag :color="getConfidenceTagColor(row.confidence)">
          {{ (row.confidence * 100).toFixed(1) }}%
        </Tag>
      </template>

      <template #cameraName="{ row }">
        <span>{{ row.cameraName || row.cameraId }}</span>
      </template>

      <template #alarmTypeId="{ row }">
        <Tag color="blue">{{ formatAlarmTypeName(row.alarmTypeId) }}</Tag>
      </template>

      <template #targetLabel="{ row }">
        <Tag v-if="row.targetLabel">{{ formatTargetClass(row.targetLabel) }}</Tag>
        <span v-else class="text-xs text-muted-foreground">-</span>
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

                <div class="flex items-center gap-1.5">
                  <span class="text-xs text-muted-foreground">
                    {{ $t('record.alarm.detail.confidence') }}:
                  </span>
                  <Tag :color="getConfidenceTagColor(currentDetail.confidence)">
                    {{ (currentDetail.confidence * 100).toFixed(1) }}%
                  </Tag>
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
                  formatDateTime(currentDetail.occurredAt) || '-'
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
