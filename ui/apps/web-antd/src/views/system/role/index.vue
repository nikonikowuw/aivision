<script lang="ts" setup>
import type { VxeTableGridOptions } from '#/adapter/vxe-table';
import type { MenuApi, RoleApi } from '#/api';
import type { SystemStatus } from '#/constants/system';

import { ref } from 'vue';

import { Page, useVbenModal } from '@vben/common-ui';
import { $t } from '@vben/locales';

import { Button, message, Popconfirm, Tag, Tree } from 'ant-design-vue';

import { useVbenForm, z } from '#/adapter/form';
import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  assignRoleMenusApi,
  batchDeleteRoleApi,
  createRoleApi,
  deleteRoleApi,
  getMenuTreeApi,
  getRoleMenuIdsApi,
  getRolePageApi,
  updateRoleApi,
} from '#/api';
import {
  SYSTEM_BUILTIN_ROLE_ID,
  SYSTEM_ROLE_ADMIN_CODE,
  SYSTEM_ROLE_SUPER_CODE,
  SYSTEM_STATUS,
} from '#/constants/system';

type RoleFormValues = Omit<RoleApi.SaveRoleInput, 'sort' | 'status'> & {
  sort: number;
  status: SystemStatus;
};

const currentEditId = ref<null | number>(null);
const selectedRoles = ref<RoleApi.RoleItem[]>([]);

// 表单配置
const [Form, formApi] = useVbenForm<RoleFormValues>({
  schema: [
    {
      component: 'Input',
      fieldName: 'name',
      label: $t('system.role.name'),
      rules: z.string().min(1, $t('system.role.nameRequired')),
    },
    {
      component: 'Input',
      componentProps: {
        placeholder: $t('system.role.codePlaceholder'),
      },
      fieldName: 'code',
      label: $t('system.role.code'),
      rules: z.string().min(1, $t('system.role.codeRequired')),
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
    {
      component: 'Textarea',
      fieldName: 'remark',
      label: $t('system.common.remark'),
    },
  ],
  showDefaultActions: false,
});

// 角色新增/编辑 Modal
const [RoleModal, roleModalApi] = useVbenModal({
  onConfirm: async () => {
    const { valid } = await formApi.validate();
    if (!valid) return;
    const values = await formApi.getValues();
    const payload: RoleApi.SaveRoleInput = {
      code: values.code,
      name: values.name,
      remark: values.remark,
      sort: values.sort,
      status: values.status,
    };
    try {
      roleModalApi.setState({ confirmLoading: true });
      await (currentEditId.value
        ? updateRoleApi(currentEditId.value, payload)
        : createRoleApi(payload));
      message.success($t('system.common.success'));
      roleModalApi.close();
      gridApi.reload();
    } catch (error) {
      console.error(error);
    } finally {
      roleModalApi.setState({ confirmLoading: false });
    }
  },
  title: $t('system.role.roleDetail'),
});

// 菜单权限分配
interface MenuTreeNode {
  key: number;
  title: string;
  children?: MenuTreeNode[];
}

const currentAssignRoleId = ref<null | number>(null);
const allMenuTree = ref<MenuTreeNode[]>([]);
const checkedMenuKeys = ref<number[]>([]);

function transformMenuToTreeData(items: MenuApi.MenuItem[]): MenuTreeNode[] {
  return items.map((item) => ({
    key: item.id,
    title: `${item.title.startsWith('routes.') ? $t(item.title) : item.title} (${item.name})`,
    children: item.children
      ? transformMenuToTreeData(item.children)
      : undefined,
  }));
}

const [AssignModal, assignModalApi] = useVbenModal({
  onConfirm: async () => {
    if (!currentAssignRoleId.value) return;
    try {
      assignModalApi.setState({ confirmLoading: true });
      await assignRoleMenusApi(
        currentAssignRoleId.value,
        checkedMenuKeys.value,
      );
      message.success($t('system.role.assignMenuSuccess'));
      assignModalApi.close();
    } catch (error) {
      console.error(error);
    } finally {
      assignModalApi.setState({ confirmLoading: false });
    }
  },
  title: $t('system.role.assignMenuTitle'),
});

function isProtectedRole(row: RoleApi.RoleItem) {
  return (
    row.id === SYSTEM_BUILTIN_ROLE_ID ||
    row.code === SYSTEM_ROLE_SUPER_CODE ||
    row.code === SYSTEM_ROLE_ADMIN_CODE
  );
}

const gridOptions: VxeTableGridOptions<RoleApi.RoleItem> = {
  checkboxConfig: {
    checkMethod: ({ row }) => !isProtectedRole(row),
    highlight: true,
  },
  columns: [
    { type: 'checkbox', width: 50, align: 'center' },
    { field: 'name', title: $t('system.role.name'), width: 160 },
    { field: 'code', title: $t('system.role.code'), width: 160 },
    { field: 'sort', title: $t('system.common.sort'), width: 80 },
    {
      field: 'status',
      slots: { default: 'status' },
      title: $t('system.common.status'),
      width: 100,
    },
    { field: 'remark', title: $t('system.common.remark'), minWidth: 160 },
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
    enabled: true,
  },
  proxyConfig: {
    ajax: {
      query: async ({ page }, formValues) => {
        return await getRolePageApi({
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
    checkboxAll: ({ records }: { records: RoleApi.RoleItem[] }) => {
      selectedRoles.value = records;
    },
    checkboxChange: ({ records }: { records: RoleApi.RoleItem[] }) => {
      selectedRoles.value = records;
    },
  },
  formOptions: {
    schema: [
      {
        component: 'Input',
        fieldName: 'name',
        label: $t('system.role.name'),
      },
      {
        component: 'Input',
        fieldName: 'code',
        label: $t('system.role.code'),
      },
      {
        component: 'Select',
        componentProps: {
          allowClear: true,
          options: [
            { label: $t('system.common.enable'), value: SYSTEM_STATUS.ENABLED },
            {
              label: $t('system.common.disable'),
              value: SYSTEM_STATUS.DISABLED,
            },
          ],
        },
        fieldName: 'status',
        label: $t('system.common.status'),
      },
    ],
  },
  gridOptions,
});

function handleAdd() {
  currentEditId.value = null;
  formApi.resetForm();
  formApi.setValues({
    sort: 0,
    status: SYSTEM_STATUS.ENABLED,
  });
  roleModalApi.setState({ title: $t('system.role.addRole') });
  roleModalApi.open();
}

function handleEdit(row: RoleApi.RoleItem) {
  currentEditId.value = row.id;
  formApi.resetForm();
  formApi.setValues({
    code: row.code,
    name: row.name,
    remark: row.remark,
    sort: row.sort,
    status: row.status,
  });
  roleModalApi.setState({ title: $t('system.role.editRole') });
  roleModalApi.open();
}

async function handleAssignMenu(row: RoleApi.RoleItem) {
  currentAssignRoleId.value = row.id;
  try {
    const [menus, assignedIds] = await Promise.all([
      getMenuTreeApi(),
      getRoleMenuIdsApi(row.id),
    ]);
    allMenuTree.value = transformMenuToTreeData(menus || []);
    checkedMenuKeys.value = assignedIds || [];
    assignModalApi.setState({
      title: `${$t('system.role.assignMenuTitle')} - ${row.name}`,
    });
    assignModalApi.open();
  } catch (error) {
    console.error(error);
  }
}

async function handleDelete(row: RoleApi.RoleItem) {
  try {
    await deleteRoleApi(row.id);
    message.success($t('system.common.success'));
    handleClearSelection();
    gridApi.reload();
  } catch (error) {
    console.error(error);
  }
}

function handleClearSelection() {
  gridApi.grid?.clearCheckboxRow();
  selectedRoles.value = [];
}

async function handleBatchDelete() {
  const ids = selectedRoles.value.map((r) => r.id);
  if (ids.length === 0) return;
  try {
    await batchDeleteRoleApi(ids);
    message.success($t('system.common.success'));
    handleClearSelection();
    gridApi.reload();
  } catch (error) {
    console.error(error);
  }
}
</script>

<template>
  <Page auto-content-height>
    <div
      v-if="selectedRoles.length > 0"
      class="mb-3 flex items-center justify-between rounded-lg border border-primary/20 bg-primary/5 px-4 py-2 text-sm"
    >
      <div class="flex items-center gap-2">
        <span class="text-foreground font-medium">
          {{
            $t('system.common.selectedCount', { count: selectedRoles.length })
          }}
        </span>
        <Button type="link" size="small" @click="handleClearSelection">
          {{ $t('system.common.clearSelection') }}
        </Button>
      </div>
      <div class="flex items-center gap-2">
        <Popconfirm
          :title="
            $t('system.common.confirmBatchDelete', {
              count: selectedRoles.length,
            })
          "
          :ok-text="$t('system.common.confirm')"
          :cancel-text="$t('system.common.cancel')"
          @confirm="handleBatchDelete"
        >
          <Button
            type="primary"
            danger
            size="small"
            v-access:code="['system:role:delete']"
          >
            {{ $t('system.common.batchDelete') }}
          </Button>
        </Popconfirm>
      </div>
    </div>

    <Grid>
      <template #toolbar-tools>
        <Button
          type="primary"
          v-access:code="['system:role:add']"
          @click="handleAdd"
        >
          {{ $t('system.role.addRole') }}
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
          v-access:code="['system:role:edit']"
          @click="() => handleEdit(row)"
        >
          {{ $t('system.common.edit') }}
        </Button>
        <Button
          type="link"
          size="small"
          v-access:code="['system:role:assign-menu']"
          @click="() => handleAssignMenu(row)"
        >
          {{ $t('system.role.assignMenu') }}
        </Button>
        <Popconfirm
          v-if="!isProtectedRole(row)"
          :title="$t('system.role.confirmDelete')"
          :ok-text="$t('system.common.confirm')"
          :cancel-text="$t('system.common.cancel')"
          @confirm="() => handleDelete(row)"
        >
          <Button
            type="link"
            danger
            size="small"
            v-access:code="['system:role:delete']"
          >
            {{ $t('system.common.delete') }}
          </Button>
        </Popconfirm>
      </template>
    </Grid>

    <RoleModal>
      <Form class="pt-4" />
    </RoleModal>

    <AssignModal>
      <div class="max-h-96 overflow-y-auto p-4">
        <Tree
          v-model:checked-keys="checkedMenuKeys"
          :tree-data="allMenuTree"
          checkable
          default-expand-all
        />
      </div>
    </AssignModal>
  </Page>
</template>
