<script lang="ts" setup>
import type { VxeTableGridOptions } from '#/adapter/vxe-table';
import type { AlgorithmApi } from '#/api';

import { ref } from 'vue';

import { Page } from '@vben/common-ui';
import { $t } from '@vben/locales';

import {
  Button,
  Card,
  Col,
  Empty,
  Pagination,
  Radio,
  Row,
  Tag,
} from 'ant-design-vue';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { getAlgorithmList } from '#/api';
import {
  formatAlarmTypeName,
  formatAlgorithmDesc,
  formatAlgorithmName,
} from '#/utils/i18n';

import SchemaModal from './components/SchemaModal.vue';
import UploadModal from './components/UploadModal.vue';
import VersionsDrawer from './components/VersionsDrawer.vue';

// Card/Table 视图切换
const viewMode = ref<'card' | 'table'>('table');
const uploadModalOpen = ref(false);
const versionsDrawerOpen = ref(false);
const schemaModalOpen = ref(false);

const cardData = ref<AlgorithmApi.AlgorithmItem[]>([]);
const cardTotal = ref(0);
const cardCurrentPage = ref(1);
const cardPageSize = ref(8);
const cardLoading = ref(false);

async function loadCardData(page = 1, pageSize = 8) {
  cardLoading.value = true;
  try {
    const res = await getAlgorithmList({
      page,
      pageSize,
    });
    cardData.value = res.items || [];
    cardTotal.value = res.total || 0;
    cardCurrentPage.value = page;
    cardPageSize.value = pageSize;
  } finally {
    cardLoading.value = false;
  }
}

const currentSelectedAlgo = ref<AlgorithmApi.AlgorithmItem | null>(null);
const currentSchema = ref<Record<string, unknown>>({});
const currentSchemaTitle = ref('');

const gridOptions: VxeTableGridOptions<AlgorithmApi.AlgorithmItem> = {
  columns: [
    {
      type: 'seq',
      title: $t('system.common.index'),
      width: 60,
      align: 'center',
    },
    {
      field: 'name',
      title: $t('ai.algorithm.name'),
      minWidth: 160,
      slots: { default: 'name' },
    },
    {
      field: 'algorithmId',
      title: $t('ai.algorithm.algorithmId'),
      minWidth: 150,
      slots: { default: 'algorithmId' },
    },
    {
      field: 'algorithmType',
      title: $t('ai.algorithm.type'),
      minWidth: 130,
      slots: { default: 'algorithmType' },
    },
    {
      field: 'activeVersion',
      title: $t('ai.algorithm.activeVersion'),
      minWidth: 120,
      slots: { default: 'activeVersion' },
    },
    {
      field: 'alarmTypeId',
      title: $t('ai.algorithm.alarmTypeId'),
      minWidth: 140,
      slots: { default: 'alarmTypeId' },
    },
    {
      field: 'description',
      title: $t('ai.algorithm.description'),
      minWidth: 200,
      showOverflow: true,
      slots: { default: 'description' },
    },
    {
      field: 'createdAt',
      title: $t('ai.algorithm.createdAt'),
      formatter: 'formatDateTime',
      minWidth: 170,
    },
    {
      field: 'actions',
      title: $t('system.common.action'),
      fixed: 'right',
      width: 170,
      showOverflow: false,
      slots: { default: 'actions' },
    },
  ],
  pagerConfig: {
    enabled: true,
  },
  proxyConfig: {
    ajax: {
      query: async ({ page }, formValues) => {
        const res = await getAlgorithmList({
          page: page.currentPage,
          pageSize: page.pageSize,
          ...formValues,
        });
        return {
          items: res.items || [],
          total: res.total || 0,
        };
      },
    },
  },
};

const [Grid, gridApi] = useVbenVxeGrid({
  formOptions: {
    schema: [
      {
        component: 'Input',
        fieldName: 'keyword',
        label: $t('ai.algorithm.algorithmId'),
        componentProps: {
          placeholder: $t('ai.algorithm.keywordPlaceholder'),
        },
      },
      {
        component: 'Select',
        fieldName: 'algorithmType',
        label: $t('ai.algorithm.type'),
        componentProps: {
          allowClear: true,
          options: [
            {
              label: $t('ai.algorithm.typeDetection'),
              value: 'object_detection',
            },
            { label: $t('ai.algorithm.typeFace'), value: 'face_recognition' },
            {
              label: $t('ai.algorithm.typePlate'),
              value: 'license_plate_recognition',
            },
          ],
        },
      },
    ],
  },
  gridOptions,
});

function handleOpenVersions(algo: AlgorithmApi.AlgorithmItem) {
  currentSelectedAlgo.value = algo;
  versionsDrawerOpen.value = true;
}

function handleOpenActiveSchema(algo: AlgorithmApi.AlgorithmItem) {
  const activeVer = algo.versions?.find(
    (v) => v.version === algo.activeVersion,
  );
  let schema = activeVer?.configSchema;
  if (typeof schema === 'string') {
    try {
      schema = JSON.parse(schema);
    } catch {
      schema = {};
    }
  }
  currentSchema.value = (schema || {}) as Record<string, unknown>;
  currentSchemaTitle.value = `[${algo.algorithmId} v${algo.activeVersion}] ${$t('ai.algorithm.schemaBtn')}`;
  schemaModalOpen.value = true;
}

function handleViewSchemaFromDrawer(
  schema: Record<string, unknown>,
  title: string,
) {
  currentSchema.value = schema;
  currentSchemaTitle.value = title;
  schemaModalOpen.value = true;
}

function handleUploadSuccess() {
  gridApi.reload();
  if (viewMode.value === 'card') {
    loadCardData(cardCurrentPage.value, cardPageSize.value);
  }
}

function handleDrawerRefresh() {
  gridApi.reload();
  if (viewMode.value === 'card') {
    loadCardData(cardCurrentPage.value, cardPageSize.value);
  }
}

function handleViewModeChange(val: any) {
  viewMode.value = val.target.value;
  if (viewMode.value === 'card') {
    loadCardData(cardCurrentPage.value, cardPageSize.value);
  }
}
</script>

<template>
  <Page auto-content-height>
    <div class="mb-3 flex items-center justify-between">
      <Radio.Group
        :value="viewMode"
        button-style="solid"
        @change="handleViewModeChange"
      >
        <Radio.Button value="table">
          {{ $t('ai.algorithm.viewTable') }}
        </Radio.Button>
        <Radio.Button value="card">
          {{ $t('ai.algorithm.viewCard') }}
        </Radio.Button>
      </Radio.Group>
      <Button
        v-if="viewMode === 'card'"
        type="primary"
        @click="uploadModalOpen = true"
      >
        {{ $t('ai.algorithm.upload') }}
      </Button>
    </div>

    <div v-show="viewMode === 'table'">
      <Grid>
        <template #toolbar-tools>
          <Button type="primary" @click="uploadModalOpen = true">
            {{ $t('ai.algorithm.upload') }}
          </Button>
        </template>

        <template #name="{ row }">
          <div class="flex items-center gap-1.5">
            <span class="font-medium">{{
              formatAlgorithmName(row.algorithmId, row.name)
            }}</span>
            <Tag v-if="row.isBuiltin" color="purple" class="text-xs">
              {{ $t('ai.algorithm.builtin') }}
            </Tag>
          </div>
        </template>

        <template #algorithmId="{ row }">
          <span class="font-mono text-xs font-semibold text-gray-800">{{
            row.algorithmId
          }}</span>
        </template>

        <template #algorithmType="{ row }">
          <Tag
            :color="
              row.algorithmType === 'object_detection'
                ? 'cyan'
                : row.algorithmType === 'face_recognition'
                  ? 'purple'
                  : row.algorithmType === 'license_plate_recognition'
                    ? 'orange'
                    : 'default'
            "
          >
            {{
              row.algorithmType === 'object_detection'
                ? $t('ai.algorithm.typeDetection')
                : row.algorithmType === 'face_recognition'
                  ? $t('ai.algorithm.typeFace')
                  : row.algorithmType === 'license_plate_recognition'
                    ? $t('ai.algorithm.typePlate')
                    : row.algorithmType
            }}
          </Tag>
        </template>

        <template #activeVersion="{ row }">
          <Tag color="success" class="font-mono font-semibold">
            {{ row.activeVersion || '-' }}
          </Tag>
        </template>

        <template #alarmTypeId="{ row }">
          <Tag v-if="row.alarmTypeId" color="blue">
            {{ formatAlarmTypeName(row.alarmTypeId) }}
          </Tag>
          <span v-else class="text-gray-400">-</span>
        </template>

        <template #description="{ row }">
          <span>{{
            formatAlgorithmDesc(row.algorithmId, row.description) || '-'
          }}</span>
        </template>

        <template #actions="{ row }">
          <Button type="link" size="small" @click="handleOpenActiveSchema(row)">
            {{ $t('ai.algorithm.schemaBtn') }}
          </Button>
          <Button type="link" size="small" @click="handleOpenVersions(row)">
            {{ $t('ai.algorithm.versionsBtn') }}
          </Button>
        </template>
      </Grid>
    </div>

    <div v-show="viewMode === 'card'" class="mt-2">
      <div v-if="cardData.length > 0">
        <Row :gutter="[16, 16]">
          <Col
            v-for="algo in cardData"
            :key="algo.id"
            :xs="24"
            :sm="12"
            :md="8"
            :lg="6"
          >
            <Card hoverable class="h-full flex flex-col justify-between">
              <template #title>
                <div class="flex items-center justify-between">
                  <div class="flex items-center gap-1.5 truncate">
                    <span class="truncate font-semibold">{{
                      formatAlgorithmName(algo.algorithmId, algo.name)
                    }}</span>
                    <Tag v-if="algo.isBuiltin" color="purple" class="text-xs">
                      {{ $t('ai.algorithm.builtin') }}
                    </Tag>
                  </div>
                  <Tag color="success" class="font-mono text-xs">
                    v{{ algo.activeVersion }}
                  </Tag>
                </div>
              </template>
              <div>
                <div class="mb-2 font-mono text-xs text-gray-500">
                  {{ algo.algorithmId }}
                </div>
                <div class="mb-2 flex flex-wrap gap-1">
                  <Tag
                    :color="
                      algo.algorithmType === 'object_detection'
                        ? 'cyan'
                        : algo.algorithmType === 'face_recognition'
                          ? 'purple'
                          : algo.algorithmType === 'license_plate_recognition'
                            ? 'orange'
                            : 'default'
                    "
                  >
                    {{
                      algo.algorithmType === 'object_detection'
                        ? $t('ai.algorithm.typeDetection')
                        : algo.algorithmType === 'face_recognition'
                          ? $t('ai.algorithm.typeFace')
                          : algo.algorithmType === 'license_plate_recognition'
                            ? $t('ai.algorithm.typePlate')
                            : algo.algorithmType
                    }}
                  </Tag>
                  <Tag v-if="algo.alarmTypeId" color="blue">
                    {{ formatAlarmTypeName(algo.alarmTypeId) }}
                  </Tag>
                </div>
                <p class="line-clamp-2 text-xs text-gray-600">
                  {{
                    formatAlgorithmDesc(algo.algorithmId, algo.description) ||
                    '-'
                  }}
                </p>
              </div>
              <template #actions>
                <span @click="handleOpenActiveSchema(algo)">{{
                  $t('ai.algorithm.schemaBtn')
                }}</span>
                <span @click="handleOpenVersions(algo)">{{
                  $t('ai.algorithm.versionsBtn')
                }}</span>
              </template>
            </Card>
          </Col>
        </Row>
        <div class="mt-4 flex justify-end">
          <Pagination
            :current="cardCurrentPage"
            :page-size="cardPageSize"
            :total="cardTotal"
            :show-total="
              (total: number) => $t('ai.algorithm.totalCount', { total })
            "
            @change="
              (page: number, pageSize: number) => loadCardData(page, pageSize)
            "
          />
        </div>
      </div>
      <Empty v-else class="py-12" />
    </div>

    <UploadModal
      v-model:open="uploadModalOpen"
      @success="handleUploadSuccess"
    />
    <VersionsDrawer
      v-model:open="versionsDrawerOpen"
      :algorithm="currentSelectedAlgo"
      @refresh="handleDrawerRefresh"
      @view-schema="handleViewSchemaFromDrawer"
    />
    <SchemaModal
      v-model:open="schemaModalOpen"
      :schema="currentSchema"
      :title="currentSchemaTitle"
    />
  </Page>
</template>
