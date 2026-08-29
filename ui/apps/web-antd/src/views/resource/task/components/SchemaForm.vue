<script lang="ts" setup>
import type { Rule } from 'ant-design-vue/es/form';

import { computed, ref, watch } from 'vue';

import { $t } from '@vben/locales';

import {
  Alert,
  Form,
  type FormInstance,
  FormItem,
  Input,
  InputNumber,
  Select,
  Switch,
  Tooltip,
} from 'ant-design-vue';

type PrimitiveValue = boolean | number | string;
type SelectValue = number | string;
type SchemaValue = PrimitiveValue | PrimitiveValue[] | undefined;
type SchemaRecord = Record<string, unknown>;
type PropertyType = 'array' | 'boolean' | 'integer' | 'number' | 'string';

interface Props {
  schema?: null | SchemaRecord;
}

const props = withDefaults(defineProps<Props>(), {
  schema: null,
});

const formData = defineModel<Record<string, unknown>>('value', {
  default: () => ({}),
});

const formRef = ref<FormInstance>();

export interface PropertyField {
  defaultValue?: SchemaValue;
  description?: string;
  enumOptions?: Array<{ label: string; value: SelectValue }>;
  itemType: Exclude<PropertyType, 'array'>;
  key: string;
  max?: number;
  maxItems?: number;
  maxLength?: number;
  min?: number;
  minItems?: number;
  minLength?: number;
  multipleOf?: number;
  pattern?: RegExp;
  required: boolean;
  title: string;
  type: PropertyType;
  uniqueItems: boolean;
}

function isRecord(value: unknown): value is SchemaRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function toPropertyType(value: unknown): PropertyType {
  switch (String(value || 'string').toLowerCase()) {
    case 'array':
    case 'boolean':
    case 'integer':
    case 'number':
      return String(value).toLowerCase() as PropertyType;
    default:
      return 'string';
  }
}

function toPrimitiveOptions(value: unknown) {
  if (!Array.isArray(value)) return undefined;
  const primitives = value.filter(
    (item): item is SelectValue =>
      typeof item === 'number' || typeof item === 'string',
  );
  return primitives.map((item) => ({ label: String(item), value: item }));
}

const properties = computed<PropertyField[]>(() => {
  if (!props.schema || !isRecord(props.schema.properties)) return [];

  const requiredKeys = new Set<string>(
    Array.isArray(props.schema.required)
      ? props.schema.required.filter(
          (item): item is string => typeof item === 'string',
        )
      : [],
  );
  const result: PropertyField[] = [];

  for (const [key, rawValue] of Object.entries(props.schema.properties)) {
    if (!isRecord(rawValue)) continue;

    const type = toPropertyType(rawValue.type);
    const items = isRecord(rawValue.items) ? rawValue.items : undefined;
    const patternSource =
      typeof rawValue.pattern === 'string' ? rawValue.pattern : undefined;
    let pattern: RegExp | undefined;
    if (patternSource) {
      try {
        pattern = new RegExp(patternSource);
      } catch {
        pattern = undefined;
      }
    }

    const itemType = toPropertyType(items?.type) as Exclude<
      PropertyType,
      'array'
    >;
    let enumOptions = toPrimitiveOptions(
      type === 'array' ? items?.enum : rawValue.enum,
    );
    if (type === 'array' && itemType === 'boolean' && !enumOptions) {
      enumOptions = [
        { label: 'true', value: 'true' },
        { label: 'false', value: 'false' },
      ];
    }

    result.push({
      key,
      title: typeof rawValue.title === 'string' ? rawValue.title : key,
      type,
      itemType,
      description:
        typeof rawValue.description === 'string' ? rawValue.description : '',
      defaultValue: rawValue.default as SchemaValue,
      required: requiredKeys.has(key),
      enumOptions,
      min: typeof rawValue.minimum === 'number' ? rawValue.minimum : undefined,
      max: typeof rawValue.maximum === 'number' ? rawValue.maximum : undefined,
      multipleOf:
        typeof rawValue.multipleOf === 'number'
          ? rawValue.multipleOf
          : undefined,
      minLength:
        typeof rawValue.minLength === 'number' ? rawValue.minLength : undefined,
      maxLength:
        typeof rawValue.maxLength === 'number' ? rawValue.maxLength : undefined,
      minItems:
        typeof rawValue.minItems === 'number' ? rawValue.minItems : undefined,
      maxItems:
        typeof rawValue.maxItems === 'number' ? rawValue.maxItems : undefined,
      uniqueItems: rawValue.uniqueItems === true,
      pattern,
    });
  }

  return result;
});

// 根据当前 schema 的 default 值，为缺失字段补齐默认值。
function applyDefaults() {
  const current = { ...(formData.value || {}) };
  let changed = false;
  for (const property of properties.value) {
    if (
      (current[property.key] === undefined ||
        current[property.key] === null ||
        current[property.key] === '') &&
      property.defaultValue !== undefined
    ) {
      current[property.key] = property.defaultValue;
      changed = true;
    }
  }
  if (changed) formData.value = current;
}

watch(() => props.schema, applyDefaults, { immediate: true });

function validationError() {
  return new Error($t('resource.task.instance.paramsInvalid'));
}

const rules = computed<Record<string, Rule[]>>(() => {
  const map: Record<string, Rule[]> = {};
  for (const property of properties.value) {
    map[property.key] = [
      {
        validator: async (_rule, value: unknown) => {
          if (
            property.required &&
            (value === undefined ||
              value === null ||
              value === '' ||
              (Array.isArray(value) && value.length === 0))
          ) {
            throw new Error(
              $t('resource.task.instance.schemaInputRequired', {
                field: property.title,
              }),
            );
          }
          if (value === undefined || value === null || value === '') return;

          if (typeof value === 'string') {
            if (
              property.minLength !== undefined &&
              value.length < property.minLength
            ) {
              throw validationError();
            }
            if (
              property.maxLength !== undefined &&
              value.length > property.maxLength
            ) {
              throw validationError();
            }
            if (property.pattern && !property.pattern.test(value)) {
              throw validationError();
            }
          }

          if (typeof value === 'number') {
            if (!Number.isFinite(value)) throw validationError();
            if (property.type === 'integer' && !Number.isInteger(value)) {
              throw validationError();
            }
            if (property.min !== undefined && value < property.min) {
              throw validationError();
            }
            if (property.max !== undefined && value > property.max) {
              throw validationError();
            }
            if (property.multipleOf !== undefined) {
              const quotient = value / property.multipleOf;
              if (Math.abs(quotient - Math.round(quotient)) > 1e-9) {
                throw validationError();
              }
            }
          }

          if (Array.isArray(value)) {
            if (
              (property.itemType === 'integer' &&
                value.some(
                  (item) => typeof item !== 'number' || !Number.isInteger(item),
                )) ||
              (property.itemType === 'number' &&
                value.some(
                  (item) => typeof item !== 'number' || !Number.isFinite(item),
                )) ||
              (property.itemType === 'boolean' &&
                value.some((item) => typeof item !== 'boolean')) ||
              (property.itemType === 'string' &&
                value.some((item) => typeof item !== 'string'))
            ) {
              throw validationError();
            }
            if (
              property.minItems !== undefined &&
              value.length < property.minItems
            ) {
              throw validationError();
            }
            if (
              property.maxItems !== undefined &&
              value.length > property.maxItems
            ) {
              throw validationError();
            }
            if (
              property.uniqueItems &&
              new Set(value.map((item) => JSON.stringify(item))).size !==
                value.length
            ) {
              throw validationError();
            }
          }
        },
      },
    ];
  }
  return map;
});

function selectValue(key: string): SelectValue | undefined {
  const value = formData.value[key];
  return typeof value === 'number' || typeof value === 'string'
    ? value
    : undefined;
}

function booleanValue(key: string): boolean {
  return formData.value[key] === true;
}

function numberValue(key: string): number | undefined {
  const value = formData.value[key];
  return typeof value === 'number' ? value : undefined;
}

function stringValue(key: string): string | undefined {
  const value = formData.value[key];
  return typeof value === 'string' ? value : undefined;
}

function arraySelectValue(key: string): SelectValue[] {
  const value = formData.value[key];
  if (!Array.isArray(value)) return [];
  return value
    .map((item) => (typeof item === 'boolean' ? String(item) : item))
    .filter(
      (item): item is SelectValue =>
        typeof item === 'number' || typeof item === 'string',
    );
}

function setValue(key: string, value: unknown) {
  formData.value[key] = value;
}

function normalizeArrayValue(property: PropertyField, values: unknown[]) {
  const primitiveValues = values.filter(
    (value): value is SelectValue =>
      typeof value === 'number' || typeof value === 'string',
  );
  if (property.itemType === 'integer') {
    formData.value[property.key] = primitiveValues.map((value) =>
      Math.trunc(Number(value)),
    );
  } else if (property.itemType === 'number') {
    formData.value[property.key] = primitiveValues.map(Number);
  } else if (property.itemType === 'boolean') {
    formData.value[property.key] = primitiveValues.map(
      (value) => value === 'true',
    );
  } else {
    formData.value[property.key] = primitiveValues.map(String);
  }
}

async function validate() {
  if (properties.value.length === 0 || !formRef.value) return true;
  return formRef.value.validate();
}

function resetFields() {
  formRef.value?.resetFields();
}

defineExpose({ applyDefaults, validate, resetFields });
</script>

<template>
  <div class="schema-form-wrapper">
    <div v-if="properties.length === 0" class="py-2">
      <Alert
        :message="$t('resource.task.instance.schemaNoParams')"
        type="info"
        show-icon
      />
    </div>
    <Form
      v-else
      ref="formRef"
      :model="formData"
      :rules="rules"
      layout="vertical"
    >
      <FormItem
        v-for="property in properties"
        :key="property.key"
        :name="property.key"
        :required="property.required"
      >
        <template #label>
          <span>
            {{ property.title }}
            <Tooltip v-if="property.description" :title="property.description">
              <span class="text-muted-foreground ml-1 cursor-help">ⓘ</span>
            </Tooltip>
          </span>
        </template>

        <Select
          v-if="property.type !== 'array' && property.enumOptions"
          :value="selectValue(property.key)"
          :options="property.enumOptions"
          class="w-full"
          :placeholder="
            property.description ||
            $t('resource.task.instance.schemaSelectPlaceholder')
          "
          @update:value="(value) => setValue(property.key, value)"
        />

        <Switch
          v-else-if="property.type === 'boolean'"
          :checked="booleanValue(property.key)"
          @update:checked="(value) => setValue(property.key, value)"
        />

        <InputNumber
          v-else-if="property.type === 'integer' || property.type === 'number'"
          :value="numberValue(property.key)"
          :precision="property.type === 'integer' ? 0 : undefined"
          :min="property.min"
          :max="property.max"
          :step="property.multipleOf"
          class="w-full"
          :placeholder="property.description || property.title"
          @update:value="(value) => setValue(property.key, value)"
        />

        <Input
          v-else-if="property.type === 'string'"
          :value="stringValue(property.key)"
          :maxlength="property.maxLength"
          class="w-full"
          :placeholder="property.description || property.title"
          @update:value="(value) => setValue(property.key, value)"
        />

        <Select
          v-else-if="property.type === 'array'"
          :value="arraySelectValue(property.key)"
          :mode="property.enumOptions ? 'multiple' : 'tags'"
          :options="property.enumOptions"
          class="w-full"
          :placeholder="
            property.description ||
            $t('resource.task.instance.schemaMultiSelectPlaceholder')
          "
          @update:value="
            (values) =>
              normalizeArrayValue(property, Array.isArray(values) ? values : [])
          "
        />
      </FormItem>
    </Form>
  </div>
</template>
