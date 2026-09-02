<script lang="ts" setup>
import type { VxeTableGridOptions } from '#/adapter/vxe-table';
import type { CaptureApi } from '#/api';

import { reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';
import { $t } from '@vben/locales';

import {
  Button,
  Drawer,
  Form,
  FormItem,
  Input,
  InputNumber,
  Segmented,
  Select,
  SelectOption,
  Space,
  Tag,
} from 'ant-design-vue';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  getCameraPageApi,
  getCaptureDetailApi,
  getCaptureListApi,
} from '#/api';
import { getTodayRange } from '#/utils/date';

import CaptureDetailDrawer from './components/CaptureDetailDrawer.vue';
import CaptureThumbnail from './components/CaptureThumbnail.vue';

const currentCapture = ref<CaptureApi.CaptureItem | null>(null);
const detailDrawerOpen = ref(false);
const advancedOpen = ref(false);
const targetType = ref<CaptureApi.TargetType>('all');
const keyword = ref('');
const advancedFilters = reactive<{
  isRecognized?: 'false' | 'true';
  maxConfidence?: number;
  maxQuality?: number;
  minConfidence?: number;
  minQuality?: number;
}>({});

const targetOptions = [
  { label: $t('record.capture.targets.all'), value: 'all' },
  { label: $t('record.capture.targets.face'), value: 'face' },
  { label: $t('record.capture.targets.person'), value: 'person' },
  { label: $t('record.capture.targets.vehicle'), value: 'vehicle' },
  { label: $t('record.capture.targets.non_motor'), value: 'non_motor' },
  { label: $t('record.capture.targets.generic'), value: 'generic' },
];

const gridOptions: VxeTableGridOptions<CaptureApi.CaptureItem> = {
  rowConfig: {
    keyField: 'id',
    isHover: true,
  },
  columns: [
    { field: 'id', title: $t('record.capture.columns.id'), width: 72 },
    {
      field: 'targetType',
      title: $t('record.capture.columns.targetType'),
      width: 104,
      align: 'center',
      slots: { default: 'targetType' },
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
      field: 'qualityScore',
      title: $t('record.capture.columns.quality'),
      width: 100,
      align: 'center',
      slots: { default: 'quality' },
    },
    {
      field: 'cameraName',
      title: $t('record.capture.columns.cameraName'),
      minWidth: 142,
    },
    {
      field: 'capturedAt',
      title: $t('record.capture.columns.capturedAt'),
      minWidth: 172,
      formatter: 'formatDateTime',
    },
    {
      field: 'trackId',
      title: $t('record.capture.columns.trackId'),
      width: 88,
      align: 'center',
      formatter: ({ cellValue }) => (cellValue ? `#${cellValue}` : '-'),
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
        const { timeRange, ...rest } = formValues || {};
        let startTime: string | undefined;
        let endTime: string | undefined;
        if (Array.isArray(timeRange) && timeRange.length === 2) {
          startTime = timeRange[0];
          endTime = timeRange[1];
        }
        return await getCaptureListApi({
          ...rest,
          ...advancedFilters,
          isRecognized:
            advancedFilters.isRecognized === undefined
              ? undefined
              : advancedFilters.isRecognized === 'true',
          endTime,
          keyword: keyword.value.trim() || undefined,
          page: page.currentPage,
          pageSize: page.pageSize,
          startTime,
          targetType: targetType.value === 'all' ? undefined : targetType.value,
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
        component: 'RangePicker',
        componentProps: {
          showTime: true,
          valueFormat: 'YYYY-MM-DDTHH:mm:ssZ',
        },
        defaultValue: getTodayRange(),
        fieldName: 'timeRange',
        label: $t('record.capture.filter.timeRange'),
      },
    ],
  },
  gridOptions,
});

function reloadGrid() {
  gridApi.reload();
}

function resetAdvancedFilters() {
  advancedFilters.isRecognized = undefined;
  advancedFilters.minQuality = undefined;
  advancedFilters.maxQuality = undefined;
  advancedFilters.minConfidence = undefined;
  advancedFilters.maxConfidence = undefined;
}

function applyAdvancedFilters() {
  advancedOpen.value = false;
  reloadGrid();
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
    <div class="capture-toolbar mb-3 flex flex-wrap items-center gap-3">
      <Segmented
        v-model:value="targetType"
        :options="targetOptions"
        class="capture-target-segmented"
        @change="reloadGrid"
      />
      <Input
        v-model:value="keyword"
        allow-clear
        class="capture-keyword"
        :placeholder="$t('record.capture.filter.keywordPlaceholder')"
        @press-enter="reloadGrid"
      >
        <template #prefix>
          <span class="text-neutral-400">⌕</span>
        </template>
      </Input>
      <Space class="ml-auto">
        <Button @click="advancedOpen = true">
          {{ $t('record.capture.actions.advancedFilter') }}
        </Button>
        <Button
          v-if="
            Object.values(advancedFilters).some((value) => value !== undefined)
          "
          type="link"
          @click="resetAdvancedFilters"
        >
          {{ $t('record.capture.actions.clearFilter') }}
        </Button>
      </Space>
    </div>

    <Grid>
      <template #targetType="{ row }">
        <Tag :color="row.targetType === 'person' ? 'blue' : 'cyan'">
          {{ $t(`record.capture.targets.${row.targetType}`) }}
        </Tag>
      </template>

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
          fit="cover"
          :height="58"
          :url="row.imageUrl"
          :bbox="row.bbox"
          :width="84"
        />
      </template>

      <template #confidence="{ row }">
        <span class="font-mono text-xs">{{ formatRatio(row.confidence) }}</span>
      </template>

      <template #quality="{ row }">
        <Tag :color="row.qualityScore >= 0.7 ? 'green' : 'orange'">
          {{ formatRatio(row.qualityScore) }}
        </Tag>
      </template>

      <template #actions="{ row }">
        <Button size="small" type="link" @click="handleViewCapture(row)">
          {{ $t('record.capture.actions.viewDetail') }}
        </Button>
      </template>
    </Grid>

    <Drawer
      v-model:open="advancedOpen"
      :title="$t('record.capture.advanced.title')"
      :width="360"
    >
      <Form layout="vertical">
        <FormItem :label="$t('record.capture.advanced.recognized')">
          <Select
            v-model:value="advancedFilters.isRecognized"
            allow-clear
            :placeholder="$t('record.capture.advanced.recognizedPlaceholder')"
          >
            <SelectOption value="true">
              {{ $t('record.capture.advanced.recognizedYes') }}
            </SelectOption>
            <SelectOption value="false">
              {{ $t('record.capture.advanced.recognizedNo') }}
            </SelectOption>
          </Select>
        </FormItem>
        <FormItem :label="$t('record.capture.advanced.quality')">
          <Space.Compact block>
            <InputNumber
              v-model:value="advancedFilters.minQuality"
              :max="1"
              :min="0"
              :placeholder="$t('record.capture.advanced.min')"
              :step="0.05"
            />
            <InputNumber
              v-model:value="advancedFilters.maxQuality"
              :max="1"
              :min="0"
              :placeholder="$t('record.capture.advanced.max')"
              :step="0.05"
            />
          </Space.Compact>
        </FormItem>
        <FormItem :label="$t('record.capture.advanced.confidence')">
          <Space.Compact block>
            <InputNumber
              v-model:value="advancedFilters.minConfidence"
              :max="1"
              :min="0"
              :placeholder="$t('record.capture.advanced.min')"
              :step="0.05"
            />
            <InputNumber
              v-model:value="advancedFilters.maxConfidence"
              :max="1"
              :min="0"
              :placeholder="$t('record.capture.advanced.max')"
              :step="0.05"
            />
          </Space.Compact>
        </FormItem>
      </Form>
      <template #footer>
        <Space>
          <Button @click="resetAdvancedFilters">
            {{ $t('record.capture.actions.reset') }}
          </Button>
          <Button type="primary" @click="applyAdvancedFilters">
            {{ $t('record.capture.actions.apply') }}
          </Button>
        </Space>
      </template>
    </Drawer>

    <CaptureDetailDrawer
      v-model:open="detailDrawerOpen"
      :capture="currentCapture"
      @register-success="reloadGrid"
    />
  </Page>
</template>

<style scoped>
.capture-keyword {
  width: min(360px, 100%);
}

.capture-target-segmented {
  max-width: 100%;
  overflow-x: auto;
}

@media (max-width: 768px) {
  .capture-keyword {
    flex: 1 1 220px;
  }
}
</style>
