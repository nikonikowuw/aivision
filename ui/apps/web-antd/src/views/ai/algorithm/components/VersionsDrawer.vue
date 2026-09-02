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
  Tooltip,
} from 'ant-design-vue';

import { IconifyIcon } from '@vben/icons';
import { formatDateTime } from '@vben/utils';

import { activateAlgorithmVersion, uninstallAlgorithmVersion } from '#/api';
import {
  formatAlarmTypeName,
  formatAlgorithmDesc,
  formatAlgorithmName,
} from '#/utils/i18n';

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
  { title: $t('ai.algorithm.version'), dataIndex: 'version', width: 120 },
  { title: $t('ai.algorithm.platform'), dataIndex: 'platformId', width: 160 },
  { title: $t('ai.algorithm.status'), dataIndex: 'isActive', width: 130 },
  { title: $t('ai.algorithm.fpsTiers'), dataIndex: 'fpsTiers', minWidth: 180 },
  { title: $t('ai.algorithm.createdAt'), dataIndex: 'createdAt', width: 160 },
  {
    title: $t('ai.algorithm.action'),
    key: 'action',
    width: 200,
    fixed: 'right' as const,
  },
]);

function formatBytes(bytes?: number): string {
  if (!bytes || bytes <= 0) return '';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${(bytes / Math.pow(k, i)).toFixed(1)} ${sizes[i]}`;
}

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

function toVersionItem(record: unknown): AlgorithmApi.AlgorithmVersionItem {
  return record as AlgorithmApi.AlgorithmVersionItem;
}

function getTypeIcon(algoType?: string) {
  switch (algoType) {
    case 'object_detection': {
      return 'lucide:scan';
    }
    case 'face_recognition': {
      return 'lucide:scan-face';
    }
    case 'license_plate_recognition': {
      return 'lucide:car';
    }
    default: {
      return 'lucide:cpu';
    }
  }
}
</script>

<template>
  <Drawer
    v-model:open="visible"
    :title="`${$t('ai.algorithm.versionList')} - ${formatAlgorithmName(algorithm?.algorithmId, algorithm?.name)}`"
    width="920px"
    destroy-on-close
  >
    <div v-if="algorithm" class="space-y-4 py-1">
      <!-- 头部概览卡片 -->
      <div
        class="rounded-xl border border-border bg-card p-4 shadow-xs"
      >
        <div class="flex items-start justify-between gap-4">
          <div class="flex items-center gap-3">
            <div
              class="flex size-11 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary shadow-xs"
            >
              <IconifyIcon
                :icon="getTypeIcon(algorithm.algorithmType)"
                class="size-6"
              />
            </div>
            <div>
              <div class="flex items-center gap-2">
                <span class="text-base font-bold text-foreground">
                  {{ formatAlgorithmName(algorithm.algorithmId, algorithm.name) }}
                </span>
                <Tag
                  v-if="algorithm.isBuiltin"
                  color="purple"
                  class="rounded-full px-2 text-xs"
                >
                  {{ $t('ai.algorithm.builtin') }}
                </Tag>
                <Tag
                  v-else
                  color="blue"
                  class="rounded-full px-2 text-xs"
                >
                  {{ $t('ai.algorithm.sourceCustom') }}
                </Tag>
              </div>
              <div class="mt-1 flex items-center gap-2 font-mono text-xs text-muted-foreground">
                <span>{{ algorithm.algorithmId }}</span>
                <span>•</span>
                <span class="text-emerald-600 dark:text-emerald-400 font-semibold">
                  {{ $t('ai.algorithm.activeVersion') }}: v{{ algorithm.activeVersion || '-' }}
                </span>
              </div>
            </div>
          </div>

          <div class="flex flex-col items-end gap-1.5">
            <Tag
              v-if="algorithm.alarmTypeId"
              color="processing"
              class="rounded-md"
            >
              <span class="flex items-center gap-1">
                <IconifyIcon icon="lucide:bell" class="size-3" />
                {{ formatAlarmTypeName(algorithm.alarmTypeId) }}
              </span>
            </Tag>
            <span class="text-xs text-muted-foreground">
              {{ $t('ai.algorithm.installedVersions') }}: {{ versions.length }}
            </span>
          </div>
        </div>

        <p
          v-if="algorithm.description"
          class="mt-3 text-xs leading-relaxed text-muted-foreground border-t border-border pt-2.5"
        >
          {{ formatAlgorithmDesc(algorithm.algorithmId, algorithm.description) }}
        </p>
      </div>

      <!-- 版本列表表格 -->
      <div v-if="versions.length > 0">
        <Table
          :columns="columns"
          :data-source="versions"
          :pagination="false"
          row-key="id"
          size="middle"
          bordered
          class="overflow-hidden rounded-xl border border-border"
        >
          <template #bodyCell="{ column, record: item }">
            <!-- 版本号 -->
            <template v-if="column.dataIndex === 'version'">
              <div class="flex items-center gap-1.5">
                <span class="font-mono font-bold text-foreground">
                  v{{ item.version }}
                </span>
                <span
                  v-if="item.packageSizeBytes"
                  class="text-[11px] text-muted-foreground"
                >
                  ({{ formatBytes(item.packageSizeBytes) }})
                </span>
              </div>
            </template>

            <!-- 平台架构 -->
            <template v-else-if="column.dataIndex === 'platformId'">
              <div class="flex items-center gap-1">
                <Tag
                  color="cyan"
                  class="flex items-center gap-1 font-mono text-xs"
                >
                  <IconifyIcon icon="lucide:cpu" class="size-3" />
                  {{ item.platformId || 'generic' }}
                </Tag>
              </div>
            </template>

            <!-- 激活状态 -->
            <template v-else-if="column.dataIndex === 'isActive'">
              <div class="flex items-center gap-1.5">
                <span
                  v-if="item.isActive"
                  class="inline-flex items-center gap-1.5 rounded-full bg-emerald-500/10 px-2 py-0.5 text-xs font-semibold text-emerald-600 dark:bg-emerald-500/20 dark:text-emerald-400"
                >
                  <span class="size-1.5 rounded-full bg-emerald-500 animate-pulse" />
                  {{ $t('ai.algorithm.statusActive') }}
                </span>
                <span
                  v-else
                  class="text-xs text-muted-foreground"
                >
                  {{ $t('ai.algorithm.statusHistory') }}
                </span>
              </div>
            </template>

            <!-- 算力与 FPS 档位 -->
            <template v-else-if="column.dataIndex === 'fpsTiers'">
              <div
                v-if="Array.isArray(item.fpsTiers) && item.fpsTiers.length > 0"
                class="flex flex-wrap gap-1"
              >
                <Tag
                  v-for="tier in item.fpsTiers"
                  :key="`${tier.fps}-${tier.units}`"
                  class="flex items-center gap-1 border-0 bg-muted font-mono text-xs text-foreground"
                >
                  <IconifyIcon icon="lucide:zap" class="size-3 text-amber-500" />
                  <span>{{ tier.fps }} FPS</span>
                  <span class="text-muted-foreground">({{ tier.units }} CU)</span>
                </Tag>
              </div>
              <span v-else class="text-muted-foreground text-xs">-</span>
            </template>

            <!-- 安装时间 -->
            <template v-else-if="column.dataIndex === 'createdAt'">
              <span class="font-mono text-xs text-muted-foreground">
                {{ formatDateTime(item.createdAt) || '-' }}
              </span>
            </template>

            <!-- 操作 -->
            <template v-else-if="column.key === 'action'">
              <Space :size="4">
                <Button
                  type="link"
                  size="small"
                  class="flex items-center gap-1 px-1 text-xs"
                  @click="handleShowSchema(toVersionItem(item))"
                >
                  <IconifyIcon icon="lucide:file-code-2" class="size-3.5" />
                  {{ $t('ai.algorithm.schemaBtn') }}
                </Button>
                <Popconfirm
                  v-if="!item.isActive"
                  :title="$t('ai.algorithm.activateConfirm')"
                  :ok-text="$t('system.common.confirm')"
                  :cancel-text="$t('system.common.cancel')"
                  @confirm="handleActivate(toVersionItem(item))"
                >
                  <Button
                    type="link"
                    size="small"
                    class="flex items-center gap-1 px-1 text-xs text-emerald-600 dark:text-emerald-400"
                  >
                    <IconifyIcon icon="lucide:play-circle" class="size-3.5" />
                    {{ $t('ai.algorithm.activate') }}
                  </Button>
                </Popconfirm>
                <Tooltip
                  v-if="item.isBuiltin || algorithm?.isBuiltin"
                  :title="$t('ai.algorithm.builtinCannotUninstall')"
                >
                  <Button type="link" danger size="small" disabled class="px-1 text-xs">
                    {{ $t('ai.algorithm.uninstall') }}
                  </Button>
                </Tooltip>
                <Popconfirm
                  v-else
                  :title="$t('ai.algorithm.uninstallConfirm')"
                  :ok-text="$t('system.common.confirm')"
                  :cancel-text="$t('system.common.cancel')"
                  @confirm="handleUninstall(toVersionItem(item))"
                >
                  <Button type="link" danger size="small" class="flex items-center gap-1 px-1 text-xs">
                    <IconifyIcon icon="lucide:trash-2" class="size-3.5" />
                    {{ $t('ai.algorithm.uninstall') }}
                  </Button>
                </Popconfirm>
              </Space>
            </template>
          </template>
        </Table>
      </div>
      <Empty v-else :description="$t('ai.algorithm.noVersions')" class="py-12" />
    </div>
  </Drawer>
</template>
