<script lang="ts" setup>
import type { AlgorithmApi } from '#/api';

import { computed } from 'vue';

import { $t } from '@vben/locales';

import {
  Button,
  Drawer,
  Empty,
  message,
  Popconfirm,
  Space,
  Table,
  Tag,
} from 'ant-design-vue';

import { activateAlgorithmVersion, uninstallAlgorithmVersion } from '#/api';
import { formatAlgorithmName } from '#/utils/i18n';

interface Props {
  open: boolean;
  algorithm: AlgorithmApi.AlgorithmItem | null;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  (e: 'refresh'): void;
  (e: 'update:open', val: boolean): void;
  (e: 'viewSchema', schema: Record<string, unknown>, title: string): void;
}>();

const visible = computed({
  get: () => props.open,
  set: (val) => emit('update:open', val),
});

const versions = computed(() => props.algorithm?.versions || []);

const columns = computed(() => [
  { title: $t('ai.algorithm.version'), dataIndex: 'version', width: 110 },
  { title: $t('ai.algorithm.platform'), dataIndex: 'platformId', width: 180 },
  { title: $t('ai.algorithm.status'), dataIndex: 'isActive', width: 100 },
  { title: $t('ai.algorithm.fpsTiers'), dataIndex: 'fpsTiers', width: 180 },
  { title: $t('ai.algorithm.createdAt'), dataIndex: 'createdAt', width: 180 },
  {
    title: $t('ai.algorithm.action'),
    key: 'action',
    width: 220,
    fixed: 'right' as const,
  },
]);

async function handleActivate(record: AlgorithmApi.AlgorithmVersionItem) {
  if (!props.algorithm) return;
  try {
    await activateAlgorithmVersion(props.algorithm.algorithmId, record.version);
    message.success(
      $t('ai.algorithm.activateSuccess', { version: record.version }),
    );
    emit('refresh');
  } catch {
    // request.ts 已展示后端错误，组件不再重复弹出相同提示。
  }
}

async function handleUninstall(record: AlgorithmApi.AlgorithmVersionItem) {
  if (!props.algorithm) return;
  try {
    await uninstallAlgorithmVersion(
      props.algorithm.algorithmId,
      record.version,
    );
    message.success(
      $t('ai.algorithm.uninstallSuccess', { version: record.version }),
    );
    emit('refresh');
  } catch {
    // request.ts 已展示后端错误，组件不再重复弹出相同提示。
  }
}

function parseFPSTiers(record: AlgorithmApi.AlgorithmVersionItem): string {
  const tiers = Array.isArray(record.fpsTiers) ? record.fpsTiers : [];
  if (tiers.length > 0) {
    return tiers.map((t) => `${t.fps}fps(${t.units}CU)`).join(', ');
  }
  return '-';
}

function handleShowSchema(record: AlgorithmApi.AlgorithmVersionItem) {
  let schema = record.configSchema;
  if (typeof schema === 'string') {
    try {
      schema = JSON.parse(schema);
    } catch {
      schema = {};
    }
  }
  emit(
    'viewSchema',
    (schema || {}) as Record<string, unknown>,
    `[${record.algorithmId} v${record.version}] ${$t('ai.algorithm.schemaBtn')}`,
  );
}
</script>

<template>
  <Drawer
    v-model:open="visible"
    :title="`${$t('ai.algorithm.versionList')} - ${formatAlgorithmName(algorithm?.algorithmId, algorithm?.name)} (${algorithm?.algorithmId || ''})`"
    width="900px"
    destroy-on-close
  >
    <div v-if="versions.length > 0" class="py-2">
      <Table
        :columns="columns"
        :data-source="versions"
        :pagination="false"
        row-key="id"
        size="middle"
        bordered
      >
        <template #bodyCell="{ column, record: item }">
          <template v-if="column.dataIndex === 'version'">
            <span class="font-semibold text-gray-800">{{ item.version }}</span>
          </template>
          <template v-else-if="column.dataIndex === 'platformId'">
            <Tag color="blue">{{ item.platformId }}</Tag>
          </template>
          <template v-else-if="column.dataIndex === 'isActive'">
            <Tag :color="item.isActive ? 'success' : 'default'">
              {{
                item.isActive
                  ? $t('ai.algorithm.statusActive')
                  : $t('ai.algorithm.statusHistory')
              }}
            </Tag>
          </template>
          <template v-else-if="column.dataIndex === 'fpsTiers'">
            <span class="text-xs text-gray-600">{{
              parseFPSTiers(item as any)
            }}</span>
          </template>
          <template v-else-if="column.key === 'action'">
            <Space>
              <Button
                type="link"
                size="small"
                @click="handleShowSchema(item as any)"
              >
                {{ $t('ai.algorithm.schemaBtn') }}
              </Button>
              <Popconfirm
                v-if="!item.isActive"
                :title="$t('ai.algorithm.activateConfirm')"
                :ok-text="$t('system.common.confirm')"
                :cancel-text="$t('system.common.cancel')"
                @confirm="handleActivate(item as any)"
              >
                <Button type="link" size="small">
                  {{ $t('ai.algorithm.activate') }}
                </Button>
              </Popconfirm>
              <Popconfirm
                :title="$t('ai.algorithm.uninstallConfirm')"
                :ok-text="$t('system.common.confirm')"
                :cancel-text="$t('system.common.cancel')"
                @confirm="handleUninstall(item as any)"
              >
                <Button type="link" danger size="small">
                  {{ $t('ai.algorithm.uninstall') }}
                </Button>
              </Popconfirm>
            </Space>
          </template>
        </template>
      </Table>
    </div>
    <Empty v-else :description="$t('ai.algorithm.noVersions')" />
  </Drawer>
</template>
