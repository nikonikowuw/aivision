<script lang="ts" setup>
import type { VxeTableGridOptions } from '#/adapter/vxe-table';
import type { CameraApi } from '#/api';

import { computed, ref } from 'vue';

import { Page, useVbenModal } from '@vben/common-ui';
import { $t } from '@vben/locales';

import {
  Alert,
  Button,
  Descriptions,
  message,
  Popconfirm,
  Space,
  Tag,
  Tooltip,
} from 'ant-design-vue';

import { useVbenForm, z } from '#/adapter/form';
import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  batchDeleteCameraApi,
  createCameraApi,
  deleteCameraApi,
  getCameraPageApi,
  probeCameraApi,
  updateCameraApi,
} from '#/api';
import { buildRtspUrl, parseRtspUrl } from '#/utils/rtsp';

type CameraFormValues = {
  address: string;
  name: string;
  password: string;
  remark: string;
  subRtspUrl?: string;
  username: string;
};

const currentEditId = ref<null | number>(null);
const currentEditCameraId = ref('');
const selectedCameras = ref<CameraApi.CameraItem[]>([]);
const probeResult = ref<CameraApi.ProbeResult | null>(null);
const probing = ref(false);
const rowProbingIds = ref<Set<number>>(new Set());

// 摄像头表单临时状态（用于实时预览和防抖自动拆分）
const formAddress = ref('');
const formUsername = ref('');
const formPassword = ref('');

// 摄像头新增/编辑表单
const [Form, formApi] = useVbenForm<CameraFormValues>({
  handleValuesChange(values, fieldsChanged) {
    if (fieldsChanged.includes('address')) {
      const address = values.address ?? '';
      if (address.includes('@')) {
        const parts = parseRtspUrl(address);
        formAddress.value = parts.address;
        formUsername.value = parts.username;
        formPassword.value = parts.password;
        formApi.setValues({
          address: parts.address,
          password: parts.password,
          username: parts.username,
        });
        return;
      }
      formAddress.value = address;
    }
    if (fieldsChanged.includes('username')) {
      formUsername.value = values.username ?? '';
    }
    if (fieldsChanged.includes('password')) {
      formPassword.value = values.password ?? '';
    }
  },
  schema: [
    {
      component: 'Input',
      fieldName: 'name',
      label: $t('resource.camera.name'),
      rules: z.string().min(1, $t('resource.camera.nameRequired')).max(128),
    },
    {
      component: 'Input',
      fieldName: 'address',
      label: $t('resource.camera.address'),
      componentProps: {
        placeholder: $t('resource.camera.addressPlaceholder'),
      },
      rules: z
        .string()
        .min(1, $t('resource.camera.addressRequired'))
        .max(2048)
        .refine(
          (val) => {
            const trimmed = val.trim();
            const lower = trimmed.toLowerCase();
            return lower.startsWith('rtsp://') && trimmed.length > 7;
          },
          { message: $t('resource.camera.addressInvalid') },
        ),
    },
    {
      component: 'Input',
      fieldName: 'username',
      label: $t('resource.camera.username'),
    },
    {
      component: 'InputPassword',
      fieldName: 'password',
      label: $t('resource.camera.password'),
    },
    {
      component: 'Input',
      fieldName: 'subRtspUrl',
      label: $t('resource.camera.subRtspUrl'),
      componentProps: {
        placeholder: $t('resource.camera.subRtspUrlPlaceholder'),
      },
    },
    {
      component: 'Textarea',
      fieldName: 'remark',
      label: $t('resource.camera.remark'),
    },
  ],
  showDefaultActions: false,
});

// 将提交的完整 URL 预览
const finalUrl = computed(() =>
  buildRtspUrl(formAddress.value, formUsername.value, formPassword.value),
);

const [CameraModal, cameraModalApi] = useVbenModal({
  onConfirm: async () => {
    const { valid } = await formApi.validate();
    if (!valid) return;
    const values = await formApi.getValues();
    const payload: CameraApi.SaveCameraInput = {
      name: values.name,
      remark: values.remark,
      rtspUrl: buildRtspUrl(values.address, values.username, values.password),
      subRtspUrl: values.subRtspUrl,
    };
    try {
      cameraModalApi.setState({ confirmLoading: true });
      await (currentEditId.value
        ? updateCameraApi(currentEditId.value, payload)
        : createCameraApi(payload));
      message.success($t('system.common.success'));
      cameraModalApi.close();
      gridApi.reload();
    } catch (error) {
      console.error(error);
    } finally {
      cameraModalApi.setState({ confirmLoading: false });
    }
  },
  title: $t('resource.camera.title'),
});

function probeStatusTag(status: CameraApi.ProbeStatus) {
  switch (status) {
    case 'failed': {
      return { color: 'error', text: $t('resource.camera.probeFailed') };
    }
    case 'success': {
      return { color: 'success', text: $t('resource.camera.probeSuccess') };
    }
    default: {
      return { color: 'default', text: $t('resource.camera.probeNever') };
    }
  }
}

function formatMedia(row: CameraApi.CameraItem): string {
  if (!row.lastCodec) return '-';
  if (row.lastWidth > 0 && row.lastHeight > 0) {
    return `${row.lastCodec} ${row.lastWidth}×${row.lastHeight}${row.lastFps > 0 ? ` @${row.lastFps}fps` : ''}`;
  }
  return row.lastCodec;
}

function failureCodeText(code: string): string {
  const key = `resource.camera.probeFailureCode.${code}` as const;
  const localized = $t(key);
  return localized === key ? code : localized;
}

function copyToClipboard(text: string) {
  navigator.clipboard
    ?.writeText(text)
    .then(() => message.success($t('resource.camera.copied')))
    .catch(() => undefined);
}

const gridOptions: VxeTableGridOptions<CameraApi.CameraItem> = {
  checkboxConfig: {
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
    { field: 'name', title: $t('resource.camera.name'), width: 140 },
    {
      field: 'cameraId',
      title: $t('resource.camera.cameraId'),
      minWidth: 160,
      slots: { default: 'cameraId' },
    },
    {
      field: 'rtspUrl',
      title: $t('resource.camera.rtspUrl'),
      minWidth: 240,
      slots: { default: 'rtspUrl' },
    },
    {
      field: 'lastProbeStatus',
      title: $t('resource.camera.lastProbeStatus'),
      width: 110,
      slots: { default: 'probeStatus' },
    },
    {
      field: 'lastProbeAt',
      formatter: 'formatDateTime',
      title: $t('resource.camera.lastProbeAt'),
      width: 170,
    },
    {
      field: 'lastSuccessTransport',
      title: $t('resource.camera.lastSuccessTransport'),
      width: 120,
      slots: { default: 'transport' },
    },
    {
      field: 'lastCodec',
      title: $t('resource.camera.lastMedia'),
      minWidth: 160,
      slots: { default: 'media' },
    },
    {
      field: 'actions',
      fixed: 'right',
      showOverflow: false,
      slots: { default: 'actions' },
      title: $t('system.common.action'),
      width: 200,
    },
  ],
  pagerConfig: {
    enabled: true,
  },
  proxyConfig: {
    ajax: {
      query: async ({ page }, formValues) => {
        return await getCameraPageApi({
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
    checkboxAll: ({ records }: { records: CameraApi.CameraItem[] }) => {
      selectedCameras.value = records;
    },
    checkboxChange: ({ records }: { records: CameraApi.CameraItem[] }) => {
      selectedCameras.value = records;
    },
  },
  formOptions: {
    schema: [
      {
        component: 'Input',
        fieldName: 'name',
        label: $t('resource.camera.name'),
      },
    ],
  },
  gridOptions,
});

function handleAdd() {
  currentEditId.value = null;
  currentEditCameraId.value = '';
  probeResult.value = null;
  formAddress.value = '';
  formUsername.value = '';
  formPassword.value = '';
  formApi.reset();
  formApi.setValues({
    name: '',
    address: '',
    username: '',
    password: '',
    remark: '',
  });
  cameraModalApi.setState({ title: $t('resource.camera.add') });
  cameraModalApi.open();
}

function handleEdit(row: CameraApi.CameraItem) {
  currentEditId.value = row.id;
  currentEditCameraId.value = row.cameraId;
  probeResult.value = null;
  formApi.reset();
  const parts = parseRtspUrl(row.rtspUrl);
  formAddress.value = parts.address;
  formUsername.value = parts.username;
  formPassword.value = parts.password;
  formApi.setValues({
    name: row.name,
    address: parts.address,
    username: parts.username,
    password: parts.password,
    remark: row.remark,
  });
  cameraModalApi.setState({ title: $t('resource.camera.edit') });
  cameraModalApi.open();
}

async function handleProbe() {
  const values = await formApi.getValues();
  const url = buildRtspUrl(values.address, values.username, values.password);
  if (!url.startsWith('rtsp://') || !url.includes('://')) {
    message.error($t('resource.camera.addressInvalid'));
    return;
  }
  probing.value = true;
  probeResult.value = null;
  try {
    const result = await probeCameraApi({
      id: currentEditId.value ?? undefined,
      protocol: 'rtsp',
      rtspUrl: url,
    });
    probeResult.value = result;
    if (result.status === 'success') {
      message.success($t('resource.camera.probeSuccessDesc'));
    } else {
      message.error(
        `${$t('resource.camera.probeFailedDesc')}: ${failureCodeText(result.failureCode)}`,
      );
    }
  } catch (error) {
    console.error(error);
    message.error($t('resource.camera.probeFailed'));
  } finally {
    probing.value = false;
  }
}

async function handleRowProbe(row: CameraApi.CameraItem) {
  if (rowProbingIds.value.has(row.id)) return;
  rowProbingIds.value.add(row.id);
  try {
    const result = await probeCameraApi({
      id: row.id,
      protocol: 'rtsp',
      rtspUrl: row.rtspUrl,
    });
    if (result.status === 'success') {
      message.success(`${row.name}: ${$t('resource.camera.probeSuccessDesc')}`);
    } else {
      message.error(
        `${row.name}: ${$t('resource.camera.probeFailedDesc')} (${failureCodeText(result.failureCode)})`,
      );
    }
    gridApi.reload();
  } catch (error) {
    console.error(error);
    message.error(`${row.name}: ${$t('resource.camera.probeFailed')}`);
  } finally {
    rowProbingIds.value.delete(row.id);
  }
}

function handleClearSelection() {
  gridApi.grid?.clearCheckboxRow();
  selectedCameras.value = [];
}

async function handleBatchDelete() {
  const ids = selectedCameras.value.map((c) => c.id);
  if (ids.length === 0) return;
  try {
    await batchDeleteCameraApi(ids);
    message.success($t('system.common.success'));
    handleClearSelection();
    gridApi.reload();
  } catch (error) {
    console.error(error);
  }
}

async function handleDelete(row: CameraApi.CameraItem) {
  try {
    await deleteCameraApi(row.id);
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
      v-if="selectedCameras.length > 0"
      class="mb-3 flex items-center justify-between rounded-lg border border-primary/20 bg-primary/5 px-4 py-2 text-sm"
    >
      <div class="flex items-center gap-2">
        <span class="text-foreground font-medium">
          {{
            $t('system.common.selectedCount', { count: selectedCameras.length })
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
              count: selectedCameras.length,
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
            v-access:code="['resource:camera:delete']"
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
          v-access:code="['resource:camera:add']"
          @click="handleAdd"
        >
          {{ $t('resource.camera.add') }}
        </Button>
      </template>

      <template #cameraId="{ row }">
        <Tooltip :title="row.cameraId">
          <span class="max-w-44 truncate font-mono text-xs">{{
            row.cameraId
          }}</span>
        </Tooltip>
        <Button
          type="link"
          size="small"
          class="ml-1"
          @click="copyToClipboard(row.cameraId)"
        >
          {{ $t('resource.camera.copyId') }}
        </Button>
      </template>

      <template #rtspUrl="{ row }">
        <Tooltip :title="row.rtspUrl">
          <span class="max-w-56 truncate font-mono text-xs">{{
            row.rtspUrl
          }}</span>
        </Tooltip>
        <Button
          type="link"
          size="small"
          class="ml-1"
          @click="copyToClipboard(row.rtspUrl)"
        >
          {{ $t('resource.camera.copyUrl') }}
        </Button>
      </template>

      <template #probeStatus="{ row }">
        <Tag :color="probeStatusTag(row.lastProbeStatus).color">
          {{ probeStatusTag(row.lastProbeStatus).text }}
        </Tag>
      </template>

      <template #transport="{ row }">
        <span v-if="row.lastSuccessTransport">
          {{
            row.lastSuccessTransport === 'udp'
              ? $t('resource.camera.udp')
              : $t('resource.camera.tcp')
          }}
        </span>
        <span v-else>-</span>
      </template>

      <template #media="{ row }">
        <span>{{ formatMedia(row) }}</span>
      </template>

      <template #actions="{ row }">
        <Button
          type="link"
          size="small"
          :loading="rowProbingIds.has(row.id)"
          v-access:code="['resource:camera:probe']"
          @click="handleRowProbe(row)"
        >
          {{ $t('resource.camera.probe') }}
        </Button>
        <Button
          type="link"
          size="small"
          v-access:code="['resource:camera:edit']"
          @click="handleEdit(row)"
        >
          {{ $t('system.common.edit') }}
        </Button>
        <Popconfirm
          :title="$t('system.common.confirmDelete')"
          :ok-text="$t('system.common.confirm')"
          :cancel-text="$t('system.common.cancel')"
          @confirm="handleDelete(row)"
        >
          <Button
            type="link"
            danger
            size="small"
            v-access:code="['resource:camera:delete']"
          >
            {{ $t('system.common.delete') }}
          </Button>
        </Popconfirm>
      </template>
    </Grid>

    <CameraModal>
      <div class="pt-2">
        <div
          v-if="currentEditCameraId"
          class="mb-3 flex items-center justify-between rounded-lg border border-border/80 bg-muted/40 px-3.5 py-2 text-xs"
        >
          <span class="text-muted-foreground">{{
            $t('resource.camera.cameraId')
          }}</span>
          <div class="flex items-center gap-1.5 font-mono text-foreground/90">
            <span>{{ currentEditCameraId }}</span>
            <Button
              type="link"
              size="small"
              class="h-auto p-0 text-xs"
              @click="copyToClipboard(currentEditCameraId)"
            >
              {{ $t('resource.camera.copyId') }}
            </Button>
          </div>
        </div>

        <Form />

        <!-- RTSP 最终提交预览与测活区域 -->
        <div
          class="mt-4 rounded-lg border border-border/80 bg-muted/40 p-3.5 transition-colors"
        >
          <div
            class="flex items-center justify-between gap-3 pb-2 border-b border-border/50"
          >
            <div
              class="text-xs font-medium text-muted-foreground flex items-center gap-1.5"
            >
              <span>{{ $t('resource.camera.finalUrl') }}</span>
            </div>
            <Button
              type="primary"
              ghost
              size="small"
              :loading="probing"
              v-access:code="['resource:camera:probe']"
              @click="handleProbe"
            >
              {{
                probing
                  ? $t('resource.camera.probing')
                  : $t('resource.camera.probe')
              }}
            </Button>
          </div>
          <div
            class="pt-2.5 break-all font-mono text-xs text-foreground/90 select-all"
          >
            {{ finalUrl || '-' }}
          </div>
        </div>

        <Alert
          v-if="probeResult"
          class="mt-3.5"
          :type="probeResult.status === 'success' ? 'success' : 'error'"
          :show-icon="true"
          :message="
            probeResult.status === 'success'
              ? $t('resource.camera.probeSuccessDesc')
              : $t('resource.camera.probeFailedDesc')
          "
        >
          <template v-if="probeResult.status === 'failed'" #description>
            <div class="mt-1 text-xs">
              {{ failureCodeText(probeResult.failureCode) }}
            </div>
          </template>
          <template v-else #description>
            <Descriptions :column="2" size="small" class="mt-2.5">
              <Descriptions.Item
                :label="$t('resource.camera.selectedTransport')"
              >
                <Tag
                  :color="
                    probeResult.selectedTransport === 'tcp' ? 'blue' : 'purple'
                  "
                  class="text-xs"
                >
                  {{
                    probeResult.selectedTransport === 'udp'
                      ? $t('resource.camera.udp')
                      : $t('resource.camera.tcp')
                  }}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item :label="$t('resource.camera.codec')">
                <Tag color="cyan" class="text-xs">{{ probeResult.codec }}</Tag>
              </Descriptions.Item>
              <Descriptions.Item :label="$t('resource.camera.media')">
                {{ probeResult.width }}×{{ probeResult.height }}
              </Descriptions.Item>
              <Descriptions.Item :label="$t('resource.camera.fps')">
                {{ probeResult.fps }} fps
              </Descriptions.Item>
              <Descriptions.Item :label="$t('resource.camera.elapsed')">
                {{ probeResult.elapsedMs }} ms
              </Descriptions.Item>
            </Descriptions>
            <Space
              v-if="probeResult.persisted || probeResult.stale"
              class="mt-2"
            >
              <Tag v-if="probeResult.persisted" color="success">
                {{ $t('resource.camera.persisted') }}
              </Tag>
              <Tag v-if="probeResult.stale" color="warning">
                {{ $t('resource.camera.stale') }}
              </Tag>
            </Space>
          </template>
        </Alert>
      </div>
    </CameraModal>
  </Page>
</template>
