<script lang="ts" setup>
import type { VxeTableGridOptions } from '#/adapter/vxe-table';
import type { PersonApi } from '#/api';

import { ref } from 'vue';

import { Page, useVbenModal } from '@vben/common-ui';
import { $t } from '@vben/locales';

import { Button, message, Popconfirm, Tag } from 'ant-design-vue';

import { useVbenForm, z } from '#/adapter/form';
import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  batchDeletePersonApi,
  createPersonApi,
  deletePersonApi,
  getPersonPageApi,
  updatePersonApi,
} from '#/api';

import PersonFaceDrawer from './components/PersonFaceDrawer.vue';

type PersonFormValues = {
  name: string;
  personId?: string;
};

const currentEditPersonId = ref<null | string>(null);
const selectedPersons = ref<PersonApi.PersonItem[]>([]);
const faceDrawerOpen = ref<boolean>(false);
const currentFacePerson = ref<null | PersonApi.PersonItem>(null);

function openFaceDrawer(record: PersonApi.PersonItem) {
  currentFacePerson.value = record;
  faceDrawerOpen.value = true;
}

function handleFaceChange() {
  gridApi.reload();
}

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
      gridApi.reload();
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
  formApi.reset();
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
    gridApi.reload();
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
    gridApi.reload();
  } catch {
    // 拦截器提示错误
  }
}

function handleClearSelection() {
  gridApi.grid?.clearCheckboxRow();
  selectedPersons.value = [];
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
      field: 'faceCount',
      slots: { default: 'faceCount' },
      title: $t('resource.person.faceCount'),
      minWidth: 110,
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
      slots: { default: 'actions' },
      title: $t('system.common.action'),
      width: 220,
    },
  ],
  pagerConfig: {
    enabled: true,
  },
  proxyConfig: {
    ajax: {
      query: async ({ page }, formValues) => {
        return await getPersonPageApi({
          page: page.currentPage,
          pageSize: page.pageSize,
          ...formValues,
        });
      },
    },
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
</script>

<template>
  <Page auto-content-height>
    <div
      v-if="selectedPersons.length > 0"
      class="mb-3 flex items-center justify-between rounded-lg border border-primary/20 bg-primary/5 px-4 py-2 text-sm"
    >
      <div class="flex items-center gap-2">
        <span class="text-foreground font-medium">
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
      <div class="flex items-center gap-2">
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
            type="primary"
            danger
            size="small"
          >
            {{ $t('system.common.batchDelete') }}
          </Button>
        </Popconfirm>
      </div>
    </div>

    <Grid>
      <template #toolbar-tools>
        <Button
          v-access:code="['resource:person:add']"
          type="primary"
          @click="openAddModal"
        >
          {{ $t('resource.person.add') }}
        </Button>
      </template>

      <template #faceCount="{ row }">
        <Tag :color="(row.faceCount || 0) > 0 ? 'processing' : 'default'">
          {{ row.faceCount || 0 }} / 10
        </Tag>
      </template>

      <template #actions="{ row }">
        <Button
          v-access:code="['resource:person:face:manage']"
          size="small"
          type="link"
          @click="openFaceDrawer(row)"
        >
          {{ $t('resource.person.faceManage') }}
        </Button>
        <Button
          v-access:code="['resource:person:edit']"
          size="small"
          type="link"
          @click="openEditModal(row)"
        >
          {{ $t('system.common.edit') }}
        </Button>
        <Popconfirm
          :title="$t('resource.person.deleteConfirm')"
          :ok-text="$t('system.common.confirm')"
          :cancel-text="$t('system.common.cancel')"
          @confirm="handleDelete(row)"
        >
          <Button
            v-access:code="['resource:person:delete']"
            danger
            size="small"
            type="link"
          >
            {{ $t('system.common.delete') }}
          </Button>
        </Popconfirm>
      </template>
    </Grid>

    <Modal>
      <Form />
    </Modal>

    <PersonFaceDrawer
      v-model:open="faceDrawerOpen"
      :person="currentFacePerson"
      @change="handleFaceChange"
    />
  </Page>
</template>
