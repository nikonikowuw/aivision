<script lang="ts" setup>
import type { VxeTableGridOptions } from '#/adapter/vxe-table';
import type { LogApi } from '#/api';

import { ref } from 'vue';

import { Page, useVbenModal } from '@vben/common-ui';
import { $t } from '@vben/locales';
import { formatDateTime } from '@vben/utils';

import { Button, Descriptions, Tag } from 'ant-design-vue';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { getLogDetailApi, getLogPageApi } from '#/api';
import { getTodayRange } from '#/utils/date';
import { translateI18nKey } from '#/utils/i18n';

const currentDetail = ref<LogApi.LogItem | null>(null);

function formatAction(action?: string) {
  if (!action) return '-';
  return translateI18nKey(action);
}

const [DetailModal, detailModalApi] = useVbenModal({
  onConfirm: () => {
    detailModalApi.close();
  },
  showCancelButton: false,
  title: $t('system.log.logDetail'),
});

const gridOptions: VxeTableGridOptions<LogApi.LogItem> = {
  columns: [
    { field: 'id', title: 'ID', width: 80 },
    { field: 'username', title: $t('system.log.operator'), width: 120 },
    { field: 'module', title: $t('system.log.module'), width: 120 },
    {
      field: 'action',
      formatter: ({ cellValue }) => formatAction(cellValue),
      minWidth: 140,
      title: $t('system.log.action'),
    },
    {
      field: 'method',
      slots: { default: 'method' },
      title: $t('system.log.method'),
      width: 100,
    },
    { field: 'path', title: $t('system.log.path'), minWidth: 180 },
    {
      field: 'statusCode',
      slots: { default: 'statusCode' },
      title: $t('system.log.statusCode'),
      width: 100,
    },
    {
      field: 'durationMs',
      formatter: ({ cellValue }) => `${cellValue} ms`,
      title: $t('system.log.duration'),
      width: 100,
    },
    { field: 'ip', title: $t('system.log.ip'), width: 130 },
    {
      field: 'createdAt',
      formatter: 'formatDateTime',
      title: $t('system.log.time'),
      width: 180,
    },
    {
      field: 'actions',
      fixed: 'right',
      showOverflow: false,
      slots: { default: 'actions' },
      title: $t('system.common.action'),
      width: 120,
    },
  ],
  pagerConfig: {
    enabled: true,
  },
  proxyConfig: {
    ajax: {
      query: async ({ page }, formValues) => {
        const { dateRange, ...rest } = formValues || {};
        let startTime: string | undefined;
        let endTime: string | undefined;
        if (dateRange && Array.isArray(dateRange) && dateRange.length === 2) {
          startTime = dateRange[0];
          endTime = dateRange[1];
        }
        return await getLogPageApi({
          endTime,
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
        component: 'Input',
        fieldName: 'username',
        label: $t('system.log.operator'),
      },
      {
        component: 'Input',
        fieldName: 'module',
        label: $t('system.log.module'),
      },
      {
        component: 'InputNumber',
        fieldName: 'statusCode',
        label: $t('system.log.statusCode'),
      },
      {
        component: 'RangePicker',
        componentProps: {
          showTime: true,
          valueFormat: 'YYYY-MM-DDTHH:mm:ssZ',
        },
        defaultValue: getTodayRange(),
        fieldName: 'dateRange',
        label: $t('system.log.time'),
      },
    ],
  },
  gridOptions,
});

async function handleViewDetail(row: LogApi.LogItem) {
  try {
    const detail = await getLogDetailApi(row.id);
    currentDetail.value = detail || row;
    detailModalApi.open();
  } catch (error) {
    console.error(error);
  }
}

function getMethodColor(method: string) {
  switch (method?.toUpperCase()) {
    case 'DELETE': {
      return 'red';
    }
    case 'GET': {
      return 'blue';
    }
    case 'POST': {
      return 'green';
    }
    case 'PUT': {
      return 'orange';
    }
    default: {
      return 'default';
    }
  }
}
</script>

<template>
  <Page auto-content-height>
    <Grid>
      <template #method="{ row }">
        <Tag :color="getMethodColor(row.method)">
          {{ row.method }}
        </Tag>
      </template>

      <template #statusCode="{ row }">
        <Tag
          :color="
            row.statusCode >= 200 && row.statusCode < 300 ? 'success' : 'error'
          "
        >
          {{ row.statusCode }}
        </Tag>
      </template>

      <template #actions="{ row }">
        <Button
          type="link"
          size="small"
          v-access:code="['system:log']"
          @click="() => handleViewDetail(row)"
        >
          {{ $t('system.log.detail') }}
        </Button>
      </template>
    </Grid>

    <DetailModal>
      <div v-if="currentDetail" class="p-4">
        <Descriptions :column="2" bordered size="small">
          <Descriptions.Item :label="$t('system.log.logId')">
            {{ currentDetail.id }}
          </Descriptions.Item>
          <Descriptions.Item :label="$t('system.log.operator')">
            {{ currentDetail.username }}
          </Descriptions.Item>
          <Descriptions.Item :label="$t('system.log.module')">
            {{ currentDetail.module }}
          </Descriptions.Item>
          <Descriptions.Item :label="$t('system.log.action')">
            {{ formatAction(currentDetail.action) }}
          </Descriptions.Item>
          <Descriptions.Item :label="$t('system.log.method')">
            <Tag :color="getMethodColor(currentDetail.method)">
              {{ currentDetail.method }}
            </Tag>
          </Descriptions.Item>
          <Descriptions.Item :label="$t('system.log.statusCode')">
            <Tag
              :color="
                currentDetail.statusCode >= 200 &&
                currentDetail.statusCode < 300
                  ? 'success'
                  : 'error'
              "
            >
              {{ currentDetail.statusCode }}
            </Tag>
          </Descriptions.Item>
          <Descriptions.Item :label="$t('system.log.duration')">
            {{ currentDetail.durationMs }} ms
          </Descriptions.Item>
          <Descriptions.Item :label="$t('system.log.ip')">
            {{ currentDetail.ip }}
          </Descriptions.Item>
          <Descriptions.Item :label="$t('system.log.path')" :span="2">
            {{ currentDetail.path }}
          </Descriptions.Item>
          <Descriptions.Item :label="$t('system.log.queryParams')" :span="2">
            <pre
              class="bg-gray-50 dark:bg-gray-800 p-2 rounded max-h-32 overflow-auto text-xs"
              >{{ currentDetail.query || '-' }}</pre>
          </Descriptions.Item>
          <Descriptions.Item :label="$t('system.log.requestBody')" :span="2">
            <pre
              class="bg-gray-50 dark:bg-gray-800 p-2 rounded max-h-48 overflow-auto text-xs"
              >{{ currentDetail.body || '-' }}</pre>
          </Descriptions.Item>
          <Descriptions.Item :label="$t('system.log.userAgent')" :span="2">
            <span class="text-xs break-all">{{ currentDetail.userAgent }}</span>
          </Descriptions.Item>
          <Descriptions.Item :label="$t('system.log.time')" :span="2">
            {{ formatDateTime(currentDetail.createdAt) }}
          </Descriptions.Item>
        </Descriptions>
      </div>
    </DetailModal>
  </Page>
</template>
