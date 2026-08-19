<script lang="ts" setup>
import type { VxeTableGridOptions } from '#/adapter/vxe-table';
import type { DeptApi, RoleApi, UserApi } from '#/api';
import type { SystemStatus } from '#/constants/system';

import { ref } from 'vue';

import { Page, useVbenModal } from '@vben/common-ui';
import { $t } from '@vben/locales';

import {
  Button,
  Checkbox,
  message,
  Popconfirm,
  Switch,
  Tooltip,
} from 'ant-design-vue';

import { useVbenForm, z } from '#/adapter/form';
import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  assignUserRolesApi,
  batchDeleteUserApi,
  batchUpdateUserStatusApi,
  createUserApi,
  deleteUserApi,
  getDeptTreeApi,
  getRolePageApi,
  getUserPageApi,
  getUserRolesApi,
  resetUserPasswordApi,
  updateUserApi,
  updateUserStatusApi,
} from '#/api';
import {
  SYSTEM_ADMIN_USER_ID,
  SYSTEM_ADMIN_USERNAME,
  SYSTEM_STATUS,
} from '#/constants/system';

type UserFormValues = Omit<UserApi.SaveUserInput, 'status'> & {
  status: SystemStatus;
};

const currentEditId = ref<null | number>(null);
const deptTreeOptions = ref<DeptApi.DeptItem[]>([]);
const selectedUsers = ref<UserApi.UserItem[]>([]);

function isProtectedUser(row: UserApi.UserItem) {
  return (
    row.id === SYSTEM_ADMIN_USER_ID || row.username === SYSTEM_ADMIN_USERNAME
  );
}

// 用户新增/编辑表单
const [Form, formApi] = useVbenForm<UserFormValues>({
  schema: [
    {
      component: 'Input',
      componentProps: () => ({
        disabled: !!currentEditId.value,
      }),
      fieldName: 'username',
      label: $t('system.user.username'),
      rules: z.string().min(2, $t('system.user.usernameMin')).max(64),
    },
    {
      component: 'InputPassword',
      componentProps: {
        placeholder: $t('system.user.initPasswordPlaceholder'),
      },
      dependencies: {
        show: () => !currentEditId.value,
        triggerFields: ['username'],
      },
      fieldName: 'password',
      label: $t('system.user.initPassword'),
    },
    {
      component: 'Input',
      fieldName: 'nickname',
      label: $t('system.user.nickname'),
    },
    {
      component: 'ApiTreeSelect',
      componentProps: () => ({
        allowClear: true,
        childrenField: 'children',
        labelField: 'name',
        options: deptTreeOptions.value,
        placeholder: $t('system.user.deptPlaceholder'),
        treeDefaultExpandAll: true,
        valueField: 'id',
        dropdownStyle: {
          maxHeight: '400px',
          overflow: 'auto',
          minWidth: '300px',
        },
        popupMatchSelectWidth: false,
      }),
      fieldName: 'deptId',
      label: $t('system.user.dept'),
    },
    {
      component: 'Input',
      fieldName: 'email',
      label: $t('system.user.email'),
    },
    {
      component: 'Input',
      fieldName: 'phone',
      label: $t('system.user.phone'),
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

const [UserModal, userModalApi] = useVbenModal({
  onConfirm: async () => {
    const { valid } = await formApi.validate();
    if (!valid) return;
    const values = await formApi.getValues();
    const payload: UserApi.SaveUserInput = {
      deptId: values.deptId,
      email: values.email,
      nickname: values.nickname,
      phone: values.phone,
      remark: values.remark,
      status: values.status,
      username: values.username,
    };
    try {
      userModalApi.setState({ confirmLoading: true });
      await (currentEditId.value
        ? updateUserApi(currentEditId.value, payload)
        : createUserApi({
            ...payload,
            password: values.password || undefined,
          }));
      message.success($t('system.common.success'));
      userModalApi.close();
      gridApi.reload();
    } catch (error) {
      console.error(error);
    } finally {
      userModalApi.setState({ confirmLoading: false });
    }
  },
  title: $t('system.user.userDetail'),
});

// 分配角色
const currentAssignUserId = ref<null | number>(null);
const allRoles = ref<RoleApi.RoleItem[]>([]);
const checkedRoleIds = ref<number[]>([]);

const [AssignRoleModal, assignRoleModalApi] = useVbenModal({
  onConfirm: async () => {
    if (!currentAssignUserId.value) return;
    try {
      assignRoleModalApi.setState({ confirmLoading: true });
      await assignUserRolesApi(currentAssignUserId.value, checkedRoleIds.value);
      message.success($t('system.user.assignRoleSuccess'));
      assignRoleModalApi.close();
    } catch (error) {
      console.error(error);
    } finally {
      assignRoleModalApi.setState({ confirmLoading: false });
    }
  },
  title: $t('system.user.assignRole'),
});

const gridOptions: VxeTableGridOptions<UserApi.UserItem> = {
  checkboxConfig: {
    checkMethod: ({ row }) => !isProtectedUser(row),
    highlight: true,
  },
  columns: [
    { type: 'checkbox', width: 50, align: 'center' },
    {
      type: 'seq',
      title: $t('system.common.index'),
      width: 60,
      align: 'center',
    },
    { field: 'username', title: $t('system.user.username'), width: 140 },
    { field: 'nickname', title: $t('system.user.nickname'), width: 140 },
    { field: 'deptName', title: $t('system.user.dept'), width: 140 },
    { field: 'phone', title: $t('system.user.phone'), width: 140 },
    { field: 'email', title: $t('system.user.email'), width: 180 },
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
      width: 360,
    },
  ],
  pagerConfig: {
    enabled: true,
  },
  proxyConfig: {
    ajax: {
      query: async ({ page }, formValues) => {
        if (deptTreeOptions.value.length === 0) {
          const depts = await getDeptTreeApi();
          deptTreeOptions.value = depts || [];
        }
        return await getUserPageApi({
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
    checkboxAll: ({ records }: { records: UserApi.UserItem[] }) => {
      selectedUsers.value = records;
    },
    checkboxChange: ({ records }: { records: UserApi.UserItem[] }) => {
      selectedUsers.value = records;
    },
  },
  formOptions: {
    schema: [
      {
        component: 'Input',
        fieldName: 'username',
        label: $t('system.user.username'),
      },
      {
        component: 'Input',
        fieldName: 'nickname',
        label: $t('system.user.nickname'),
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
      {
        component: 'ApiTreeSelect',
        componentProps: () => ({
          allowClear: true,
          childrenField: 'children',
          labelField: 'name',
          options: deptTreeOptions.value,
          placeholder: $t('system.user.deptFilterPlaceholder'),
          treeDefaultExpandAll: true,
          valueField: 'id',
          dropdownStyle: {
            maxHeight: '400px',
            overflow: 'auto',
            minWidth: '300px',
          },
          popupMatchSelectWidth: false,
        }),
        fieldName: 'deptId',
        label: $t('system.user.dept'),
      },
    ],
  },
  gridOptions,
});

function handleAdd() {
  currentEditId.value = null;
  formApi.resetForm();
  formApi.setValues({
    status: SYSTEM_STATUS.ENABLED,
  });
  userModalApi.setState({ title: $t('system.user.addUser') });
  userModalApi.open();
}

function handleEdit(row: UserApi.UserItem) {
  currentEditId.value = row.id;
  formApi.resetForm();
  formApi.setValues({
    deptId: row.deptId,
    email: row.email,
    nickname: row.nickname,
    phone: row.phone,
    remark: row.remark,
    status: row.status,
    username: row.username,
  });
  userModalApi.setState({ title: $t('system.user.editUser') });
  userModalApi.open();
}

async function handleStatusChange(row: UserApi.UserItem, checked: boolean) {
  const newStatus = checked ? SYSTEM_STATUS.ENABLED : SYSTEM_STATUS.DISABLED;
  try {
    await updateUserStatusApi(row.id, newStatus);
    row.status = newStatus;
    const statusText = checked
      ? $t('system.common.enable')
      : $t('system.common.disable');
    message.success(`${statusText} - ${$t('system.common.success')}`);
  } catch (error) {
    console.error(error);
  }
}

function handleClearSelection() {
  gridApi.grid?.clearCheckboxRow();
  selectedUsers.value = [];
}

async function handleBatchDelete() {
  const ids = selectedUsers.value.map((u) => u.id);
  if (ids.length === 0) return;
  try {
    await batchDeleteUserApi(ids);
    message.success($t('system.common.success'));
    handleClearSelection();
    gridApi.reload();
  } catch (error) {
    console.error(error);
  }
}

async function handleResetPassword(row: UserApi.UserItem) {
  try {
    await resetUserPasswordApi(row.id);
    message.success($t('system.user.resetPasswordSuccess'));
  } catch (error) {
    console.error(error);
  }
}

const ROLE_PAGE_SIZE = 100;

async function getAllRoles() {
  const roles: RoleApi.RoleItem[] = [];
  let page = 1;
  let total: number;

  do {
    const result = await getRolePageApi({
      page,
      pageSize: ROLE_PAGE_SIZE,
    });
    const pageItems = result.items || [];
    roles.push(...pageItems);
    total = result.total;
    page += 1;

    if (pageItems.length === 0) break;
  } while (roles.length < total);

  return roles;
}

async function handleAssignRole(row: UserApi.UserItem) {
  currentAssignUserId.value = row.id;
  try {
    const [roles, userRoles] = await Promise.all([
      getAllRoles(),
      getUserRolesApi(row.id),
    ]);
    allRoles.value = roles;
    checkedRoleIds.value = userRoles || [];
    assignRoleModalApi.setState({
      title: `${$t('system.user.assignRole')} - ${row.username}`,
    });
    assignRoleModalApi.open();
  } catch (error) {
    console.error(error);
  }
}

async function handleDelete(row: UserApi.UserItem) {
  try {
    await deleteUserApi(row.id);
    message.success($t('system.common.success'));
    handleClearSelection();
    gridApi.reload();
  } catch (error) {
    console.error(error);
  }
}

async function handleBatchStatusChange(status: SystemStatus) {
  const ids = selectedUsers.value.map((u) => u.id);
  if (ids.length === 0) return;
  try {
    await batchUpdateUserStatusApi(ids, status);
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
      v-if="selectedUsers.length > 0"
      class="mb-3 flex items-center justify-between rounded-lg border border-primary/20 bg-primary/5 px-4 py-2 text-sm"
    >
      <div class="flex items-center gap-2">
        <span class="text-foreground font-medium">
          {{
            $t('system.common.selectedCount', { count: selectedUsers.length })
          }}
        </span>
        <Button type="link" size="small" @click="handleClearSelection">
          {{ $t('system.common.clearSelection') }}
        </Button>
      </div>
      <div class="flex items-center gap-2">
        <Button
          size="small"
          v-access:code="['system:user:status']"
          @click="() => handleBatchStatusChange(SYSTEM_STATUS.ENABLED)"
        >
          {{ $t('system.common.batchEnable') }}
        </Button>
        <Button
          size="small"
          v-access:code="['system:user:status']"
          @click="() => handleBatchStatusChange(SYSTEM_STATUS.DISABLED)"
        >
          {{ $t('system.common.batchDisable') }}
        </Button>
        <Popconfirm
          :title="
            $t('system.common.confirmBatchDelete', {
              count: selectedUsers.length,
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
            v-access:code="['system:user:delete']"
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
          v-access:code="['system:user:add']"
          @click="handleAdd"
        >
          {{ $t('system.user.addUser') }}
        </Button>
      </template>

      <template #status="{ row }">
        <Tooltip
          v-if="
            row.id === SYSTEM_ADMIN_USER_ID ||
            row.username === SYSTEM_ADMIN_USERNAME
          "
          :title="$t('system.user.adminProtectedTip')"
        >
          <Switch
            v-access:code="['system:user:status']"
            :checked="row.status === SYSTEM_STATUS.ENABLED"
            disabled
          />
        </Tooltip>
        <Switch
          v-else
          v-access:code="['system:user:status']"
          :checked="row.status === SYSTEM_STATUS.ENABLED"
          @change="(checked) => handleStatusChange(row, !!checked)"
        />
      </template>

      <template #actions="{ row }">
        <Button
          type="link"
          size="small"
          v-access:code="['system:user:edit']"
          @click="() => handleEdit(row)"
        >
          {{ $t('system.common.edit') }}
        </Button>
        <Button
          type="link"
          size="small"
          v-access:code="['system:user:assign-role']"
          @click="() => handleAssignRole(row)"
        >
          {{ $t('system.user.assignRole') }}
        </Button>
        <Popconfirm
          :title="$t('system.user.confirmResetPassword')"
          :ok-text="$t('system.common.confirm')"
          :cancel-text="$t('system.common.cancel')"
          @confirm="() => handleResetPassword(row)"
        >
          <Button
            type="link"
            size="small"
            v-access:code="['system:user:reset-password']"
          >
            {{ $t('system.user.resetPassword') }}
          </Button>
        </Popconfirm>
        <Tooltip
          v-if="
            row.id === SYSTEM_ADMIN_USER_ID ||
            row.username === SYSTEM_ADMIN_USERNAME
          "
          :title="$t('system.user.adminProtectedTip')"
        >
          <Button
            type="link"
            danger
            size="small"
            disabled
            v-access:code="['system:user:delete']"
          >
            {{ $t('system.common.delete') }}
          </Button>
        </Tooltip>
        <Popconfirm
          v-else
          :title="$t('system.common.confirmDelete')"
          :ok-text="$t('system.common.confirm')"
          :cancel-text="$t('system.common.cancel')"
          @confirm="() => handleDelete(row)"
        >
          <Button
            type="link"
            danger
            size="small"
            v-access:code="['system:user:delete']"
          >
            {{ $t('system.common.delete') }}
          </Button>
        </Popconfirm>
      </template>
    </Grid>

    <UserModal>
      <Form class="pt-4" />
    </UserModal>

    <AssignRoleModal>
      <div class="p-4">
        <Checkbox.Group
          v-model:value="checkedRoleIds"
          class="flex flex-col gap-2"
        >
          <Checkbox v-for="role in allRoles" :key="role.id" :value="role.id">
            {{ role.name }} ({{ role.code }})
          </Checkbox>
        </Checkbox.Group>
      </div>
    </AssignRoleModal>
  </Page>
</template>
