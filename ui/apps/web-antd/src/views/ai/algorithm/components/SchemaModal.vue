<script lang="ts" setup>
import { computed } from 'vue';

import { $t } from '@vben/locales';

import { Modal, Table } from 'ant-design-vue';

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

const dataSource = computed<SchemaPropertyRow[]>(() => {
  if (!props.schema || typeof props.schema !== 'object') {
    return [];
  }
  const schemaObj = props.schema as {
    properties?: Record<string, any>;
    required?: string[];
  };
  const properties = schemaObj.properties || {};
  const requiredList: string[] = Array.isArray(schemaObj.required)
    ? schemaObj.required
    : [];

  return Object.keys(properties).map((key) => {
    const field = properties[key] || {};
    let desc = field.description || '-';
    if (field.minimum !== undefined || field.maximum !== undefined) {
      desc += ` [${field.minimum ?? '-'}, ${field.maximum ?? '-'}]`;
    }
    if (field.enum) {
      desc += ` ${JSON.stringify(field.enum)}`;
    }

    return {
      key,
      title: field.title || key,
      type: field.type || 'any',
      defaultValue:
        field.default === undefined ? '-' : JSON.stringify(field.default),
      required: requiredList.includes(key),
      description: desc,
    };
  });
});
</script>

<template>
  <Modal
    v-model:open="visible"
    :title="modalTitle"
    :footer="null"
    width="850px"
    destroy-on-close
  >
    <div class="py-4">
      <div v-if="dataSource.length > 0">
        <Table
          :columns="columns"
          :data-source="dataSource"
          :pagination="false"
          row-key="key"
          size="middle"
          bordered
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.dataIndex === 'required'">
              <span
                :class="
                  record.required ? 'text-red-500 font-bold' : 'text-gray-400'
                "
              >
                {{
                  record.required
                    ? $t('ai.algorithm.propYes')
                    : $t('ai.algorithm.propNo')
                }}
              </span>
            </template>
            <template v-else-if="column.dataIndex === 'type'">
              <span
                class="rounded bg-blue-50 px-1.5 py-0.5 text-xs text-blue-600"
              >
                {{ record.type }}
              </span>
            </template>
            <template v-else-if="column.dataIndex === 'key'">
              <code class="text-xs font-semibold text-gray-800">{{
                record.key
              }}</code>
            </template>
          </template>
        </Table>
      </div>
      <div v-else class="py-8 text-center text-gray-400">
        {{ $t('ai.algorithm.noSchema') }}
      </div>
    </div>
  </Modal>
</template>
