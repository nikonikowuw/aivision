<script lang="ts" setup>
import { computed, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';
import { $t } from '@vben/locales';

import {
  Button,
  Empty,
  Modal,
  Table,
  Tabs,
  Tag,
  Tooltip,
} from 'ant-design-vue';

import { copyToClipboard } from '#/utils/clipboard';
import { formatPropertyDesc, formatPropertyTitle } from '#/utils/i18n';

interface Props {
  open: boolean;
  schema: null | Record<string, unknown>;
  title?: string;
}

const props = withDefaults(defineProps<Props>(), {
  title: '',
});

const emit = defineEmits<{
  (e: 'update:open', val: boolean): void;
}>();

const visible = computed({
  get: () => props.open,
  set: (val) => emit('update:open', val),
});

const activeTab = ref<'raw' | 'visual'>('visual');

const modalTitle = computed(
  () => props.title || $t('ai.algorithm.schemaModalTitle'),
);

interface SchemaPropertyRow {
  key: string;
  title: string;
  type: string;
  defaultValue: string;
  required: boolean;
  description: string;
  minimum?: number;
  maximum?: number;
  enumOptions?: unknown[];
}

const columns = computed(() => [
  { title: $t('ai.algorithm.propKey'), dataIndex: 'key', width: 160 },
  { title: $t('ai.algorithm.propTitle'), dataIndex: 'title', width: 140 },
  { title: $t('ai.algorithm.propType'), dataIndex: 'type', width: 100 },
  {
    title: $t('ai.algorithm.propDefault'),
    dataIndex: 'defaultValue',
    width: 120,
  },
  { title: $t('ai.algorithm.propRequired'), dataIndex: 'required', width: 80 },
  { title: $t('ai.algorithm.propDesc'), dataIndex: 'description' },
]);

const formattedJson = computed(() => {
  if (!props.schema) return '{}';
  return JSON.stringify(props.schema, null, 2);
});

const dataSource = computed<SchemaPropertyRow[]>(() => {
  if (!props.schema || typeof props.schema !== 'object') {
    return [];
  }
  const schemaObj = props.schema as {
    properties?: Record<string, Record<string, unknown>>;
    required?: string[];
  };
  const properties = schemaObj.properties || {};
  const requiredList: string[] = Array.isArray(schemaObj.required)
    ? schemaObj.required
    : [];

  return Object.keys(properties).map((key) => {
    const field = properties[key] || {};
    const rawDesc =
      typeof field.description === 'string' ? field.description : undefined;
    const baseDesc = formatPropertyDesc(key, rawDesc) || rawDesc || '-';

    return {
      key,
      title: formatPropertyTitle(
        key,
        typeof field.title === 'string' ? field.title : undefined,
      ),
      type: typeof field.type === 'string' ? field.type : 'any',
      defaultValue:
        field.default === undefined ? '-' : JSON.stringify(field.default),
      required: requiredList.includes(key),
      description: baseDesc,
      minimum: typeof field.minimum === 'number' ? field.minimum : undefined,
      maximum: typeof field.maximum === 'number' ? field.maximum : undefined,
      enumOptions: Array.isArray(field.enum) ? field.enum : undefined,
    };
  });
});

function getTypeColor(type: string): string {
  switch (type.toLowerCase()) {
    case 'array': {
      return 'orange';
    }
    case 'boolean': {
      return 'purple';
    }
    case 'integer':
    case 'number': {
      return 'blue';
    }
    case 'object': {
      return 'cyan';
    }
    case 'string': {
      return 'green';
    }
    default: {
      return 'default';
    }
  }
}

async function handleCopyJson() {
  await copyToClipboard(formattedJson.value, $t('ai.algorithm.copySuccess'));
}
</script>

<template>
  <Modal
    v-model:open="visible"
    :title="modalTitle"
    :footer="null"
    width="900px"
    destroy-on-close
  >
    <div class="py-2">
      <div class="mb-4 flex items-center justify-between">
        <Tabs v-model:active-key="activeTab" size="small" class="w-full">
          <Tabs.TabPane key="visual" :tab="$t('ai.algorithm.visualTable')" />
          <Tabs.TabPane key="raw" :tab="$t('ai.algorithm.rawJson')" />
        </Tabs>
      </div>

      <!-- 结构化表格视图 -->
      <div v-if="activeTab === 'visual'">
        <div v-if="dataSource.length > 0">
          <Table
            :columns="columns"
            :data-source="dataSource"
            :pagination="false"
            row-key="key"
            size="middle"
            bordered
            class="overflow-hidden rounded-lg"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.dataIndex === 'key'">
                <span
                  class="font-mono text-xs font-semibold text-sky-600 dark:text-sky-400"
                >
                  {{ record.key }}
                </span>
              </template>
              <template v-else-if="column.dataIndex === 'title'">
                <span class="font-medium text-foreground">{{
                  record.title
                }}</span>
              </template>
              <template v-else-if="column.dataIndex === 'type'">
                <Tag
                  :color="getTypeColor(record.type)"
                  class="font-mono text-xs"
                >
                  {{ record.type }}
                </Tag>
              </template>
              <template v-else-if="column.dataIndex === 'defaultValue'">
                <code
                  v-if="record.defaultValue !== '-'"
                  class="rounded bg-muted px-1.5 py-0.5 font-mono text-xs text-foreground"
                >
                  {{ record.defaultValue }}
                </code>
                <span v-else class="text-muted-foreground">-</span>
              </template>
              <template v-else-if="column.dataIndex === 'required'">
                <Tag
                  v-if="record.required"
                  color="error"
                  class="text-xs font-semibold"
                >
                  {{ $t('ai.algorithm.propYes') }}
                </Tag>
                <span v-else class="text-muted-foreground text-xs">
                  {{ $t('ai.algorithm.propNo') }}
                </span>
              </template>
              <template v-else-if="column.dataIndex === 'description'">
                <div class="space-y-1">
                  <div class="text-xs text-foreground">
                    {{ record.description }}
                  </div>
                  <div
                    v-if="
                      record.minimum !== undefined ||
                      record.maximum !== undefined
                    "
                    class="flex items-center gap-1"
                  >
                    <span class="text-[11px] text-muted-foreground">Range:</span>
                    <span
                      class="rounded bg-muted px-1 py-0.2 font-mono text-[11px] text-primary"
                    >
                      [{{ record.minimum ?? '-∞' }},
                      {{ record.maximum ?? '+∞' }}]
                    </span>
                  </div>
                  <div
                    v-if="record.enumOptions && record.enumOptions.length > 0"
                    class="flex flex-wrap items-center gap-1"
                  >
                    <span class="text-[11px] text-muted-foreground">Enum:</span>
                    <Tag
                      v-for="opt in record.enumOptions"
                      :key="String(opt)"
                      class="font-mono text-[10px]"
                    >
                      {{ JSON.stringify(opt) }}
                    </Tag>
                  </div>
                </div>
              </template>
            </template>
          </Table>
        </div>
        <Empty
          v-else
          :description="$t('ai.algorithm.noSchemaConfig')"
          class="py-12"
        />
      </div>

      <!-- JSON 原文视图 -->
      <div v-else class="relative">
        <div class="absolute right-3 top-3 z-10">
          <Tooltip :title="$t('ai.algorithm.copySchema')">
            <Button
              size="small"
              class="flex items-center gap-1 shadow-xs"
              @click="handleCopyJson"
            >
              <IconifyIcon icon="lucide:copy" class="size-3.5" />
              <span>{{ $t('system.common.copy') }}</span>
            </Button>
          </Tooltip>
        </div>
        <pre
          class="max-h-[500px] overflow-auto rounded-lg bg-muted/70 p-4 font-mono text-xs leading-relaxed text-foreground border border-border"
        ><code>{{ formattedJson }}</code></pre>
      </div>
    </div>
  </Modal>
</template>
