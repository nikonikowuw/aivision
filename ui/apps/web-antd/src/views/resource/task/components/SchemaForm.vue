<script lang="ts" setup>
import type { Rule } from 'ant-design-vue/es/form';

import { computed, ref, watch } from 'vue';

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

interface Props {
  schema?: null | Record<string, any>;
}

const props = withDefaults(defineProps<Props>(), {
  schema: null,
});

const formData = defineModel<Record<string, any>>('value', {
  default: () => ({}),
});

const formRef = ref<FormInstance>();

export interface PropertyField {
  defaultValue?: any;
  description?: string;
  enumOptions?: Array<{ label: string; value: number | string }>;
  key: string;
  max?: number;
  maxLength?: number;
  min?: number;
  minLength?: number;
  required: boolean;
  step?: number;
  title: string;
  type: 'array' | 'boolean' | 'integer' | 'number' | 'string';
}

const properties = computed<PropertyField[]>(() => {
  if (
    !props.schema ||
    typeof props.schema !== 'object' ||
    !props.schema.properties
  ) {
    return [];
  }
  const requiredKeys = new Set<string>(
    Array.isArray(props.schema.required) ? props.schema.required : [],
  );
  const result: PropertyField[] = [];

  for (const [key, rawProp] of Object.entries<any>(props.schema.properties)) {
    if (!rawProp || typeof rawProp !== 'object') continue;

    const rawType = String(rawProp.type || 'string').toLowerCase();
    let type: PropertyField['type'] = 'string';
    if (rawType === 'boolean') type = 'boolean';
    else if (rawType === 'integer') type = 'integer';
    else if (rawType === 'number') type = 'number';
    else if (rawType === 'array') type = 'array';
    else type = 'string';

    let enumOptions: PropertyField['enumOptions'];
    if (Array.isArray(rawProp.enum)) {
      enumOptions = rawProp.enum.map((v: any) => ({
        label: String(v),
        value: v,
      }));
    } else if (rawProp.items && Array.isArray(rawProp.items.enum)) {
      enumOptions = rawProp.items.enum.map((v: any) => ({
        label: String(v),
        value: v,
      }));
    }

    result.push({
      key,
      title: rawProp.title || key,
      type,
      description: rawProp.description || '',
      defaultValue: rawProp.default,
      required: requiredKeys.has(key),
      enumOptions,
      min: typeof rawProp.minimum === 'number' ? rawProp.minimum : undefined,
      max: typeof rawProp.maximum === 'number' ? rawProp.maximum : undefined,
      step:
        typeof rawProp.multipleOf === 'number' ? rawProp.multipleOf : undefined,
      minLength:
        typeof rawProp.minLength === 'number' ? rawProp.minLength : undefined,
      maxLength:
        typeof rawProp.maxLength === 'number' ? rawProp.maxLength : undefined,
    });
  }

  return result;
});

// 初始化默认值
watch(
  properties,
  (propsList) => {
    const current = { ...(formData.value || {}) };
    let changed = false;
    for (const prop of propsList) {
      if (current[prop.key] === undefined && prop.defaultValue !== undefined) {
        current[prop.key] = prop.defaultValue;
        changed = true;
      }
    }
    if (changed) {
      formData.value = current;
    }
  },
  { immediate: true },
);

const rules = computed<Record<string, Rule[]>>(() => {
  const map: Record<string, Rule[]> = {};
  for (const prop of properties.value) {
    const fieldRules: Rule[] = [];
    if (prop.required) {
      fieldRules.push({
        required: true,
        message: `请输入或选择 ${prop.title}`,
      });
    }
    if (prop.type === 'string' && prop.minLength) {
      fieldRules.push({
        min: prop.minLength,
        message: `${prop.title} 最小长度为 ${prop.minLength}`,
      });
    }
    if (prop.type === 'string' && prop.maxLength) {
      fieldRules.push({
        max: prop.maxLength,
        message: `${prop.title} 最大长度为 ${prop.maxLength}`,
      });
    }
    if (fieldRules.length > 0) {
      map[prop.key] = fieldRules;
    }
  }
  return map;
});

async function validate() {
  if (properties.value.length === 0) return true;
  if (!formRef.value) return true;
  return formRef.value.validate();
}

function resetFields() {
  formRef.value?.resetFields();
}

defineExpose({
  validate,
  resetFields,
});
</script>

<template>
  <div class="schema-form-wrapper">
    <div v-if="properties.length === 0" class="py-2">
      <Alert message="该算法无自定义动态参数" type="info" show-icon />
    </div>
    <Form
      v-else
      ref="formRef"
      :model="formData"
      :rules="rules"
      layout="vertical"
    >
      <FormItem
        v-for="prop in properties"
        :key="prop.key"
        :name="prop.key"
        :required="prop.required"
      >
        <template #label>
          <span>
            {{ prop.title }}
            <Tooltip v-if="prop.description" :title="prop.description">
              <span class="text-muted-foreground ml-1 cursor-help">ⓘ</span>
            </Tooltip>
          </span>
        </template>

        <!-- boolean 开关 -->
        <Switch
          v-if="prop.type === 'boolean'"
          v-model:checked="formData[prop.key]"
        />

        <!-- integer 数字 -->
        <InputNumber
          v-else-if="prop.type === 'integer'"
          v-model:value="formData[prop.key]"
          :precision="0"
          :min="prop.min"
          :max="prop.max"
          class="w-full"
          :placeholder="prop.description || prop.title"
        />

        <!-- number 浮点数 -->
        <InputNumber
          v-else-if="prop.type === 'number'"
          v-model:value="formData[prop.key]"
          :min="prop.min"
          :max="prop.max"
          :step="prop.step"
          class="w-full"
          :placeholder="prop.description || prop.title"
        />

        <!-- 枚举下拉选择 -->
        <Select
          v-else-if="prop.type === 'string' && prop.enumOptions"
          v-model:value="formData[prop.key]"
          :options="prop.enumOptions"
          class="w-full"
          :placeholder="prop.description || '请选择'"
        />

        <!-- 普通单行文本 -->
        <Input
          v-else-if="prop.type === 'string'"
          v-model:value="formData[prop.key]"
          :maxlength="prop.maxLength"
          class="w-full"
          :placeholder="prop.description || prop.title"
        />

        <!-- 数组多选 -->
        <Select
          v-else-if="prop.type === 'array' && prop.enumOptions"
          v-model:value="formData[prop.key]"
          mode="multiple"
          :options="prop.enumOptions"
          class="w-full"
          :placeholder="prop.description || '请选择一项或多项'"
        />

        <!-- 普通 fallback -->
        <Input
          v-else
          v-model:value="formData[prop.key]"
          class="w-full"
          :placeholder="prop.description || prop.title"
        />
      </FormItem>
    </Form>
  </div>
</template>
