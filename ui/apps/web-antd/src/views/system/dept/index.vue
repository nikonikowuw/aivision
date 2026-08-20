<script lang="ts" setup>
import type { VxeGridProps } from '#/adapter/vxe-table';
import type { DeptApi } from '#/api';
import type { SystemStatus } from '#/constants/system';

import { ref } from 'vue';

import { Page, useVbenModal } from '@vben/common-ui';
import { $t } from '@vben/locales';

import { Button, message, Popconfirm, Tag } from 'ant-design-vue';

import { useVbenForm, z } from '#/adapter/form';
import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  createDeptApi,
  deleteDeptApi,
  getDeptTreeApi,
  updateDeptApi,
} from '#/api';
import { SYSTEM_STATUS } from '#/constants/system';

type DeptFormValues = Omit<DeptApi.SaveDeptInput, 'sort' | 'status'> & {
  sort: number;
  status: SystemStatus;
};

interface DeptTreeOption {
  id: number;
  name: string;
  children?: DeptTreeOption[];
}

const currentEditId = ref<null | number>(null);
const deptTreeOptions = ref<DeptTreeOption[]>([]);

const [Form, formApi] = useVbenForm<DeptFormValues>({
  schema: [
    {
      component: 'ApiTreeSelect',
      componentProps: () => ({
        allowClear: true,
        childrenField: 'children',
        labelField: 'name',
        options: deptTreeOptions.value,
        placeholder: $t('system.dept.parentPlaceholder'),
        treeDefaultExpandAll: true,
        valueField: 'id',
        dropdownStyle: {
          maxHeight: '400px',
          overflow: 'auto',
          minWidth: '300px',
        },
        popupMatchSelectWidth: false,
      }),
      fieldName: 'parentId',
      label: $t('system.dept.parent'),
    },
    {
      component: 'Input',
      fieldName: 'name',
      label: $t('system.dept.name'),
      rules: z.string().min(1, $t('system.dept.nameRequired')),
    },
    {
      component: 'Input',
      fieldName: 'leader',
      label: $t('system.dept.leader'),
    },
    {
      component: 'Input',
      fieldName: 'phone',
      label: $t('system.dept.phone'),
    },
    {
      component: 'InputNumber',
      defaultValue: 0,
      fieldName: 'sort',
      label: $t('system.common.sort'),
    },
    {
      component: 'RadioGroup',
      componentProps: {
        options: [
          { label: $t('system.common.enable'), value: SYSTEM_STATUS.ENABLED },
          {
            label: $t('system.common.disable'),
            value: SYSTEM_STATUS.DISABLED,
          },
        ],
      },
      defaultValue: SYSTEM_STATUS.ENABLED,
      fieldName: 'status',
      label: $t('system.common.status'),
    },
  ],
  showDefaultActions: false,
});

const [Modal, modalApi] = useVbenModal({
  onConfirm: async () => {
    const { valid } = await formApi.validate();
    if (!valid) return;
    const values = await formApi.getValues();
    const payload: DeptApi.SaveDeptInput = {
      leader: values.leader,
      name: values.name,
      parentId: values.parentId || 0,
      phone: values.phone,
      sort: values.sort,
      status: values.status,
    };
    try {
      modalApi.setState({ confirmLoading: true });
      await (currentEditId.value
        ? updateDeptApi(currentEditId.value, payload)
        : createDeptApi(payload));
      message.success($t('system.common.success'));
      modalApi.close();
      gridApi.reload();
    } catch (error) {
      console.error(error);
    } finally {
      modalApi.setState({ confirmLoading: false });
    }
  },
  title: $t('system.dept.deptDetail'),
});

const gridOptions: VxeGridProps<DeptApi.DeptItem> = {
  columns: [
    {
      field: 'name',
      title: $t('system.dept.name'),
      treeNode: true,
      width: 220,
    },
    { field: 'leader', title: $t('system.dept.leader'), width: 140 },
    { field: 'phone', title: $t('system.dept.phone'), width: 160 },
    { field: 'sort', title: $t('system.common.sort'), width: 80 },
    {
      field: 'status',
      slots: { default: 'status' },
      title: $t('system.common.status'),
      width: 100,
    },
    {
      field: 'createdAt',
      formatter: 'formatDateTime',
      minWidth: 180,
      title: $t('system.common.createTime'),
    },
    {
      field: 'actions',
      fixed: 'right',
      showOverflow: false,
      slots: { default: 'actions' },
      title: $t('system.common.action'),
      width: 280,
    },
  ],
  pagerConfig: {
    enabled: false,
  },
  proxyConfig: {
    ajax: {
      query: async () => {
        const res = await getDeptTreeApi();
        const tree = res || [];
        deptTreeOptions.value = [
          {
            id: 0,
            name: $t('system.dept.topLevel'),
            children: tree,
          },
        ];
        return { items: tree, total: tree.length };
      },
    },
  },
  treeConfig: {
    childrenField: 'children',
    rowField: 'id',
    transform: false,
  },
};

const [Grid, gridApi] = useVbenVxeGrid({ gridOptions });

function handleAdd(parentId = 0) {
  currentEditId.value = null;
  formApi.resetForm();
  formApi.setValues({
    parentId,
    sort: 0,
    status: SYSTEM_STATUS.ENABLED,
  });
  modalApi.setState({ title: $t('system.dept.addDept') });
  modalApi.open();
}

function handleEdit(row: DeptApi.DeptItem) {
  currentEditId.value = row.id;
  formApi.resetForm();
  formApi.setValues({
    leader: row.leader,
    name: row.name,
    parentId: row.parentId,
    phone: row.phone,
    sort: row.sort,
    status: row.status,
  });
  modalApi.setState({ title: $t('system.dept.editDept') });
  modalApi.open();
}

async function handleDelete(row: DeptApi.DeptItem) {
  try {
    await deleteDeptApi(row.id);
    message.success($t('system.common.success'));
    gridApi.reload();
  } catch (error) {
    console.error(error);
  }
}
</script>

<template>
  <Page auto-content-height>
    <Grid>
      <template #toolbar-tools>
        <Button
          type="primary"
          v-access:code="['system:dept:add']"
          @click="() => handleAdd(0)"
        >
          {{ $t('system.dept.addDept') }}
        </Button>
      </template>

      <template #status="{ row }">
        <Tag
          :color="row.status === SYSTEM_STATUS.ENABLED ? 'success' : 'error'"
        >
          {{
            row.status === SYSTEM_STATUS.ENABLED
              ? $t('system.common.enable')
              : $t('system.common.disable')
          }}
        </Tag>
      </template>

      <template #actions="{ row }">
        <Button
          type="link"
          size="small"
          v-access:code="['system:dept:add']"
          @click="() => handleAdd(row.id)"
        >
          {{ $t('system.dept.addSubDept') }}
        </Button>
        <Button
          type="link"
          size="small"
          v-access:code="['system:dept:edit']"
          @click="() => handleEdit(row)"
        >
          {{ $t('system.common.edit') }}
        </Button>
        <Popconfirm
          :title="$t('system.dept.confirmDelete')"
          :ok-text="$t('system.common.confirm')"
          :cancel-text="$t('system.common.cancel')"
          @confirm="() => handleDelete(row)"
        >
          <Button
            type="link"
            danger
            size="small"
            v-access:code="['system:dept:delete']"
          >
            {{ $t('system.common.delete') }}
          </Button>
        </Popconfirm>
      </template>
    </Grid>

    <Modal>
      <Form class="pt-4" />
    </Modal>
  </Page>
</template>
