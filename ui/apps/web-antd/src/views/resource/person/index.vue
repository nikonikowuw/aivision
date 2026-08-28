<script lang="ts" setup>
import type { VxeTableGridOptions } from '#/adapter/vxe-table';
import type { PersonApi } from '#/api';

import { onActivated, ref } from 'vue';

import { Page, useVbenModal, VbenLoading } from '@vben/common-ui';
import { $t } from '@vben/locales';

import { Alert, Button, message, Popconfirm, Space } from 'ant-design-vue';

import { useVbenForm, z } from '#/adapter/form';
import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  batchDeletePersonApi,
  createPersonApi,
  deletePersonApi,
  getPersonPageApi,
  updatePersonApi,
} from '#/api';

type PersonFormValues = {
  name: string;
  personId?: string;
};

const GRID_MIN_LOADING_TIME = 200;

const currentEditPersonId = ref<null | string>(null);
const selectedPersons = ref<PersonApi.PersonItem[]>([]);

// 人员新增/编辑表单
const [Form, formApi] = useVbenForm<PersonFormValues>({
  schema: [
    {
      component: 'Input',
      fieldName: 'personId',
      label: $t('resource.person.personId'),
      componentProps: () => ({
        disabled: Boolean(currentEditPersonId.value),
        placeholder: $t('resource.person.personIdPlaceholder'),
      }),
      rules: z
        .string()
        .max(64)
        .optional()
        .refine(
          (val) => {
            if (!val || val.trim() === '') return true;
            const trimmed = val.trim();
            const first = trimmed[0];
            if (!first || !/^[a-zA-Z0-9]$/.test(first)) return false;
            return /^[a-zA-Z0-9._\-:]+$/.test(trimmed);
          },
          { message: $t('resource.person.personIdInvalid') },
        ),
    },
    {
      component: 'Input',
      fieldName: 'name',
      label: $t('resource.person.name'),
      rules: z
        .string()
        .min(1, $t('resource.person.nameRequired'))
        .max(64, $t('resource.person.nameMaxLength'))
        .refine(
          (val) => {
            for (let i = 0; i < val.length; i++) {
              const code = val.codePointAt(i);
              if (
                code === undefined ||
                code < 32 ||
                (code >= 127 && code <= 159)
              )
                return false;
            }
            return true;
          },
          { message: $t('resource.person.nameInvalid') },
        ),
    },
  ],
  showDefaultActions: false,
});

// 人员新增/编辑弹窗
const [Modal, modalApi] = useVbenModal({
  onCancel() {
    modalApi.close();
  },
  onConfirm: async () => {
    const { valid } = await formApi.validate();
    if (!valid) return;

    const values = await formApi.getValues();
    try {
      modalApi.setState({ confirmLoading: true });
      if (currentEditPersonId.value) {
        await updatePersonApi(currentEditPersonId.value, {
          name: values.name.trim(),
        });
        message.success($t('system.common.success'));
      } else {
        await createPersonApi({
          name: values.name.trim(),
          personId: values.personId?.trim() || undefined,
        });
        message.success($t('system.common.success'));
      }
      modalApi.close();
      gridApi.query();
    } catch {
      // 错误由拦截器统一提示
    } finally {
      modalApi.setState({ confirmLoading: false });
    }
  },
});

function openAddModal() {
  currentEditPersonId.value = null;
  formApi.reset();
  modalApi.setState({ title: $t('resource.person.add') });
  modalApi.open();
}

function openEditModal(record: PersonApi.PersonItem) {
  currentEditPersonId.value = record.personId;
  formApi.setValues({
    name: record.name,
    personId: record.personId,
  });
  modalApi.setState({ title: $t('resource.person.edit') });
  modalApi.open();
}

async function handleDelete(record: PersonApi.PersonItem) {
  try {
    await deletePersonApi(record.personId);
    message.success($t('system.common.success'));
    handleClearSelection();
    gridApi.query();
  } catch {
    // 拦截器提示错误
  }
}

async function handleBatchDelete() {
  if (selectedPersons.value.length === 0) return;
  try {
    const personIds = selectedPersons.value.map((p) => p.personId);
    await batchDeletePersonApi({ personIds });
    message.success($t('system.common.success'));
    handleClearSelection();
    gridApi.query();
  } catch {
    // 拦截器提示错误
  }
}

const gridOptions: VxeTableGridOptions<PersonApi.PersonItem> = {
  checkboxConfig: {
    highlight: true,
  },
  columns: [
    { type: 'checkbox', width: 50, align: 'center' },
    {
      field: 'personId',
      title: $t('resource.person.personId'),
      minWidth: 200,
    },
    {
      field: 'name',
      title: $t('resource.person.name'),
      minWidth: 150,
    },
    {
      field: 'createdAt',
      formatter: 'formatDateTime',
      title: $t('resource.person.createdAt'),
      minWidth: 160,
    },
    {
      field: 'updatedAt',
      formatter: 'formatDateTime',
      title: $t('resource.person.updatedAt'),
      minWidth: 160,
    },
    {
      field: 'actions',
      fixed: 'right',
      showOverflow: false,
      slots: { default: 'action' },
      title: $t('resource.person.action'),
      width: 200,
    },
  ],
  height: 'auto',
  pagerConfig: {
    enabled: true,
  },
  proxyConfig: {
    ajax: {
      query: async ({ page }, formValues) => {
        // 翻页或筛选后清空 VXE 实际勾选状态，避免本地数组与表格状态不一致。
        handleClearSelection();
        return await getPersonPageApi({
          page: page.currentPage,
          pageSize: page.pageSize,
          ...formValues,
        });
      },
    },
  },
  toolbarConfig: {
    search: true,
  },
};

const [Grid, gridApi] = useVbenVxeGrid({
  gridEvents: {
    checkboxAll: ({ records }: { records: PersonApi.PersonItem[] }) => {
      selectedPersons.value = records;
    },
    checkboxChange: ({ records }: { records: PersonApi.PersonItem[] }) => {
      selectedPersons.value = records;
    },
  },
  formOptions: {
    schema: [
      {
        component: 'Input',
        fieldName: 'personId',
        label: $t('resource.person.personId'),
        componentProps: {
          placeholder: $t('resource.person.personIdPlaceholder'),
        },
      },
      {
        component: 'Input',
        fieldName: 'name',
        label: $t('resource.person.name'),
        componentProps: {
          placeholder: $t('resource.person.name'),
        },
      },
    ],
  },
  gridOptions,
});

// 清空人员选择，同时同步清理 VXE Grid 的内部 checkbox 状态。
function handleClearSelection() {
  gridApi.grid?.clearCheckboxRow?.();
  selectedPersons.value = [];
}

// KeepAlive 页面再次激活时刷新当前筛选和分页，避免缓存列表过期。
let hasActivated = false;
onActivated(() => {
  if (!hasActivated) {
    hasActivated = true;
    return;
  }
  handleClearSelection();
  void gridApi.query();
});
</script>

<template>
  <Page auto-content-height>
    <div class="h-full flex flex-col gap-3">
      <Alert
        v-if="selectedPersons.length > 0"
        class="mb-1"
        show-icon
        type="info"
      >
        <template #message>
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-2">
              <span>
                {{
                  $t('system.common.selectedCount', {
                    count: selectedPersons.length,
                  })
                }}
              </span>
              <Button type="link" size="small" @click="handleClearSelection">
                {{ $t('system.common.clearSelection') }}
              </Button>
            </div>
            <Popconfirm
              :title="
                $t('resource.person.batchDeleteConfirm', {
                  count: selectedPersons.length,
                })
              "
              :ok-text="$t('system.common.confirm')"
              :cancel-text="$t('system.common.cancel')"
              @confirm="handleBatchDelete"
            >
              <Button
                v-access:code="['resource:person:delete']"
                danger
                size="small"
                type="link"
              >
                {{ $t('common.delete') }}
              </Button>
            </Popconfirm>
          </div>
        </template>
      </Alert>

      <Grid>
        <template #loading>
          <VbenLoading
            :min-loading-time="GRID_MIN_LOADING_TIME"
            :spinning="true"
          />
        </template>
        <template #toolbar-tools>
          <Space>
            <Button
              v-access:code="['resource:person:add']"
              type="primary"
              @click="openAddModal"
            >
              {{ $t('resource.person.add') }}
            </Button>
          </Space>
        </template>

        <template #action="{ row }">
          <Space>
            <Button
              v-access:code="['resource:person:edit']"
              size="small"
              type="link"
              @click="openEditModal(row)"
            >
              {{ $t('common.edit') }}
            </Button>
            <Popconfirm
              :title="$t('resource.person.deleteConfirm')"
              @confirm="handleDelete(row)"
            >
              <Button
                v-access:code="['resource:person:delete']"
                danger
                size="small"
                type="link"
              >
                {{ $t('common.delete') }}
              </Button>
            </Popconfirm>
          </Space>
        </template>
      </Grid>
    </div>

    <Modal>
      <Form />
    </Modal>
  </Page>
</template>
