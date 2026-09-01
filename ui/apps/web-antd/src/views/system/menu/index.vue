<script lang="ts" setup>
import type { VxeGridProps } from '#/adapter/vxe-table';
import type { MenuApi } from '#/api';
import type { SystemStatus } from '#/constants/system';

import { ref } from 'vue';

import { Page, useVbenModal } from '@vben/common-ui';
import { $t } from '@vben/locales';

import { Button, message, Popconfirm, Tag } from 'ant-design-vue';

import { useVbenForm, z } from '#/adapter/form';
import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  createMenuApi,
  deleteMenuApi,
  getMenuTreeApi,
  updateMenuApi,
} from '#/api';
import { SYSTEM_STATUS } from '#/constants/system';
import { translateMenuLabel } from '#/utils/menu';

type MenuFormValues = Omit<MenuApi.SaveMenuInput, 'sort' | 'status'> & {
  sort: number;
  status: SystemStatus;
};

interface MenuTreeOption {
  id: number;
  name: string;
  title: string;
  children?: MenuTreeOption[];
}

const currentEditId = ref<null | number>(null);
const menuTreeOptions = ref<MenuTreeOption[]>([]);

const [Form, formApi] = useVbenForm<MenuFormValues>({
  schema: [
    {
      component: 'RadioGroup',
      componentProps: {
        buttonStyle: 'solid',
        optionType: 'button',
        options: [
          { label: $t('system.menu.catalog'), value: 'catalog' },
          { label: $t('system.menu.menu'), value: 'menu' },
          { label: $t('system.menu.button'), value: 'button' },
        ],
      },
      defaultValue: 'menu',
      fieldName: 'type',
      label: $t('system.menu.type'),
    },
    {
      component: 'ApiTreeSelect',
      componentProps: () => ({
        allowClear: true,
        childrenField: 'children',
        labelFn: (item) => {
          if (item.type === 'button') {
            return translateMenuLabel(item.name);
          }
          const title = translateMenuLabel(item.title);
          return item.name ? `${title} (${item.name})` : title;
        },
        options: menuTreeOptions.value,
        placeholder: $t('system.menu.parentPlaceholder'),
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
      label: $t('system.menu.parent'),
    },
    {
      component: 'Input',
      dependencies: {
        resolve: ({ values }) => ({
          componentProps: {
            placeholder:
              values.type === 'button'
                ? $t('system.menu.nameButtonPlaceholder')
                : $t('system.menu.namePlaceholder'),
          },
          label:
            values.type === 'button'
              ? $t('system.menu.buttonName')
              : $t('system.menu.name'),
        }),
        triggerFields: ['type'],
      },
      fieldName: 'name',
      label: $t('system.menu.name'),
      rules: z.string().min(1, $t('system.menu.nameRequired')),
    },
    {
      component: 'Input',
      componentProps: {
        placeholder: $t('system.menu.menuTitlePlaceholder'),
      },
      dependencies: {
        rules: (values) =>
          values.type === 'button'
            ? z.string().optional()
            : z.string().min(1, $t('system.menu.titleRequired')),
        show: (values) => values.type !== 'button',
        triggerFields: ['type'],
      },
      fieldName: 'title',
      label: $t('system.menu.menuTitle'),
    },
    {
      component: 'Input',
      componentProps: {
        placeholder: $t('system.menu.pathPlaceholder'),
      },
      dependencies: {
        show: (values) => values.type !== 'button',
        triggerFields: ['type'],
      },
      fieldName: 'path',
      label: $t('system.menu.path'),
    },
    {
      component: 'Input',
      componentProps: {
        placeholder: $t('system.menu.componentPlaceholder'),
      },
      dependencies: {
        show: (values) => values.type !== 'button',
        triggerFields: ['type'],
      },
      fieldName: 'component',
      label: $t('system.menu.component'),
    },
    {
      component: 'Input',
      componentProps: {
        placeholder: $t('system.menu.permissionPlaceholder'),
      },
      fieldName: 'permission',
      label: $t('system.menu.permission'),
    },
    {
      component: 'IconPicker',
      dependencies: {
        show: (values) => values.type !== 'button',
        triggerFields: ['type'],
      },
      fieldName: 'icon',
      label: $t('system.menu.icon'),
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
    const payload: MenuApi.SaveMenuInput = {
      affix: values.affix,
      component: values.component,
      homePath: values.homePath,
      icon: values.icon,
      keepAlive: values.keepAlive,
      name: values.name,
      parentId: values.parentId || 0,
      permission: values.permission,
      path: values.path,
      sort: values.sort,
      status: values.status,
      title: values.type === 'button' ? '' : values.title,
      type: values.type,
    };
    try {
      modalApi.setState({ confirmLoading: true });
      await (currentEditId.value
        ? updateMenuApi(currentEditId.value, payload)
        : createMenuApi(payload));
      message.success($t('system.common.success'));
      modalApi.close();
      gridApi.reload();
    } catch (error) {
      console.error(error);
    } finally {
      modalApi.setState({ confirmLoading: false });
    }
  },
  title: $t('system.menu.menuDetail'),
});

const gridOptions: VxeGridProps<MenuApi.MenuItem> = {
  columns: [
    {
      field: 'title',
      formatter: ({ row }) => {
        if (row.type === 'button') {
          return translateMenuLabel(row.name);
        }
        return translateMenuLabel(row.title || row.name);
      },
      title: $t('system.menu.menuTitle'),
      treeNode: true,
      width: 220,
    },
    {
      field: 'name',
      formatter: ({ row }) => (row.type === 'button' ? '-' : row.name),
      title: $t('system.menu.name'),
      width: 140,
    },
    {
      field: 'type',
      slots: { default: 'type' },
      title: $t('system.menu.type'),
      width: 90,
    },
    { field: 'path', title: $t('system.menu.path'), minWidth: 150 },
    { field: 'component', title: $t('system.menu.component'), minWidth: 180 },
    { field: 'permission', title: $t('system.menu.permission'), minWidth: 160 },
    { field: 'sort', title: $t('system.common.sort'), width: 70 },
    {
      field: 'status',
      slots: { default: 'status' },
      title: $t('system.common.status'),
      width: 90,
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
        const res = await getMenuTreeApi();
        const tree = res || [];
        menuTreeOptions.value = [
          {
            id: 0,
            name: '',
            title: $t('system.menu.topLevel'),
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
  formApi.reset();
  formApi.setValues({
    parentId,
    sort: 0,
    status: SYSTEM_STATUS.ENABLED,
    type: 'menu',
  });
  modalApi.setState({ title: $t('system.menu.addMenu') });
  modalApi.open();
}

function handleEdit(row: MenuApi.MenuItem) {
  currentEditId.value = row.id;
  formApi.reset();
  formApi.setValues({
    affix: row.affix,
    component: row.component,
    homePath: row.homePath,
    icon: row.icon,
    keepAlive: row.keepAlive,
    name: row.name,
    parentId: row.parentId,
    permission: row.permission,
    path: row.path,
    sort: row.sort,
    status: row.status,
    title: row.title,
    type: row.type,
  });
  modalApi.setState({ title: $t('system.menu.editMenu') });
  modalApi.open();
}

async function handleDelete(row: MenuApi.MenuItem) {
  try {
    await deleteMenuApi(row.id);
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
          v-access:code="['system:menu:add']"
          @click="() => handleAdd(0)"
        >
          {{ $t('system.menu.addMenu') }}
        </Button>
      </template>

      <template #type="{ row }">
        <Tag v-if="row.type === 'catalog'" color="blue">
          {{ $t('system.menu.catalog') }}
        </Tag>
        <Tag v-else-if="row.type === 'menu'" color="green">
          {{ $t('system.menu.menu') }}
        </Tag>
        <Tag v-else color="orange">
          {{ $t('system.menu.button') }}
        </Tag>
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
          v-access:code="['system:menu:add']"
          @click="() => handleAdd(row.id)"
        >
          {{ $t('system.menu.addSubItem') }}
        </Button>
        <Button
          type="link"
          size="small"
          v-access:code="['system:menu:edit']"
          @click="() => handleEdit(row)"
        >
          {{ $t('system.common.edit') }}
        </Button>
        <Popconfirm
          :title="$t('system.menu.confirmDelete')"
          :ok-text="$t('system.common.confirm')"
          :cancel-text="$t('system.common.cancel')"
          @confirm="() => handleDelete(row)"
        >
          <Button
            type="link"
            danger
            size="small"
            v-access:code="['system:menu:delete']"
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
