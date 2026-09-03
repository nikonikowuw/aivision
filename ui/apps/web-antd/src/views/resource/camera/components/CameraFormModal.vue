<script setup lang="ts">
import type { CameraApi } from '#/api/core/camera';

import { computed, reactive, ref, watch } from 'vue';

import { IconifyIcon } from '@vben/icons';
import { $t } from '@vben/locales';

import {
  Button,
  Input,
  InputNumber,
  message,
  Modal,
  RadioButton,
  RadioGroup,
  Textarea,
} from 'ant-design-vue';

import {
  createCameraApi,
  probeCameraApi,
  updateCameraApi,
} from '#/api/core/camera';
import { buildRtspUrl, parseRtspUrl } from '#/utils/rtsp';

interface Props {
  camera?: CameraApi.CameraItem | null;
  open?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  camera: null,
  open: false,
});

const emit = defineEmits<{
  (e: 'update:open', val: boolean): void;
  (e: 'success'): void;
  (e: 'preview', cam: CameraApi.CameraItem): void;
}>();

type VendorType = 'custom' | 'dahua' | 'hikvision' | 'tplink' | 'uniview';

interface FormState {
  address: string;
  channel: number;
  ip: string;
  name: string;
  password: string;
  port: number;
  remark: string;
  subRtspUrl: string;
  username: string;
  vendor: VendorType;
}

const defaultState = (): FormState => ({
  address: '',
  channel: 1,
  ip: '',
  name: '',
  password: '',
  port: 554,
  remark: '',
  subRtspUrl: '',
  username: 'admin',
  vendor: 'custom',
});

const formState = reactive<FormState>(defaultState());
const submitting = ref(false);
const probing = ref(false);
const probeResult = ref<CameraApi.ProbeResult | null>(null);

const isEdit = computed(() => Boolean(props.camera?.id));

// 自动生成主流厂商的标准 RTSP 路径
function computeVendorUrls() {
  const { channel, ip, port, vendor } = formState;
  if (vendor === 'custom' || !ip.trim()) return;

  const hostPort = port === 554 || !port ? ip.trim() : `${ip.trim()}:${port}`;

  if (vendor === 'hikvision') {
    formState.address = `rtsp://${hostPort}/Streaming/Channels/${channel}01`;
    formState.subRtspUrl = `rtsp://${hostPort}/Streaming/Channels/${channel}02`;
  } else if (vendor === 'dahua') {
    formState.address = `rtsp://${hostPort}/cam/realmonitor?channel=${channel}&subtype=0`;
    formState.subRtspUrl = `rtsp://${hostPort}/cam/realmonitor?channel=${channel}&subtype=1`;
  } else if (vendor === 'uniview') {
    formState.address = `rtsp://${hostPort}/unicast/c${channel}/s0/live`;
    formState.subRtspUrl = `rtsp://${hostPort}/unicast/c${channel}/s1/live`;
  } else if (vendor === 'tplink') {
    formState.address = `rtsp://${hostPort}/stream1`;
    formState.subRtspUrl = `rtsp://${hostPort}/stream2`;
  }
}

function handleVendorChange(vendor: VendorType) {
  formState.vendor = vendor;
  if (vendor !== 'custom') {
    if (!formState.ip && formState.address) {
      // 尝试从现有 address 中推导 IP
      try {
        const cleaned = formState.address.replace(/^rtsp:\/\//i, '');
        const hostPart = cleaned.split('/')[0] || '';
        const [h, p] = hostPart.split(':');
        if (h) formState.ip = h;
        if (p) formState.port = Number.parseInt(p, 10) || 554;
      } catch {
        // 忽略解析错误
      }
    }
    computeVendorUrls();
  }
}

function handleAddressChange(e: Event) {
  const val = (e.target as HTMLInputElement).value || '';
  if (val.includes('@')) {
    const parts = parseRtspUrl(val);
    formState.address = parts.address;
    formState.username = parts.username;
    formState.password = parts.password;
  }
}

const finalMainUrl = computed(() =>
  buildRtspUrl(formState.address, formState.username, formState.password),
);

const finalSubUrl = computed(() => {
  if (!formState.subRtspUrl?.trim()) return '';
  return buildRtspUrl(
    formState.subRtspUrl,
    formState.username,
    formState.password,
  );
});

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

async function handleProbe() {
  const url = finalMainUrl.value;
  if (!url.startsWith('rtsp://') || url.length < 8) {
    message.error($t('resource.camera.addressInvalid'));
    return;
  }
  probing.value = true;
  probeResult.value = null;
  try {
    const result = await probeCameraApi({
      id: props.camera?.id,
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
  } catch (error: any) {
    console.error(error);
    const isTimeout =
      error?.code === 'ECONNABORTED' ||
      error?.message?.includes('timeout') ||
      error?.message?.includes('超时');
    probeResult.value = {
      status: 'failed',
      failureCode: isTimeout ? 'RTSP_PLAY_TIMEOUT' : 'RTSP_CONNECT_FAILED',
      failureMessage: isTimeout ? '请求超时，设备未响应' : '网络连接失败',
      selectedTransport: 'tcp',
      attempts: [],
      codec: '',
      width: 0,
      height: 0,
      fps: 0,
      elapsedMs: 0,
      stale: false,
      persisted: false,
    };
    message.error($t('resource.camera.probeFailed'));
  } finally {
    probing.value = false;
  }
}

async function handleSave() {
  if (!formState.name.trim()) {
    message.error($t('resource.camera.nameRequired'));
    return;
  }
  const mainUrl = finalMainUrl.value.trim();
  if (!mainUrl.startsWith('rtsp://') || mainUrl.length < 8) {
    message.error($t('resource.camera.addressInvalid'));
    return;
  }

  const payload: CameraApi.SaveCameraInput = {
    name: formState.name.trim(),
    remark: formState.remark?.trim() || '',
    rtspUrl: mainUrl,
    subRtspUrl: finalSubUrl.value.trim() || undefined,
  };

  submitting.value = true;
  try {
    isEdit.value && props.camera?.id
      ? await updateCameraApi(props.camera.id, payload)
      : await createCameraApi(payload);
    message.success($t('system.common.success'));
    emit('success');
    emit('update:open', false);
  } catch (error) {
    console.error(error);
  } finally {
    submitting.value = false;
  }
}

function handleVerifyStream() {
  if (props.camera) {
    emit('preview', props.camera);
  } else {
    // 尚未持久化的摄像头临时预览
    message.info('请先保存摄像头配置以开启实时流抽检');
  }
}

watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) {
      probeResult.value = null;
      if (props.camera) {
        const parts = parseRtspUrl(props.camera.rtspUrl);
        formState.name = props.camera.name;
        formState.address = parts.address;
        formState.username = parts.username;
        formState.password = parts.password;
        formState.subRtspUrl = props.camera.subRtspUrl || '';
        formState.remark = props.camera.remark || '';
        formState.vendor = 'custom';
        formState.ip = '';
        formState.port = 554;
        formState.channel = 1;
      } else {
        Object.assign(formState, defaultState());
      }
    }
  },
);
</script>

<template>
  <Modal
    :open="open"
    :title="isEdit ? $t('resource.camera.edit') : $t('resource.camera.add')"
    :width="700"
    :confirm-loading="submitting"
    centered
    destroy-on-close
    @cancel="emit('update:open', false)"
    @ok="handleSave"
  >
    <div class="space-y-4 pt-2">
      <!-- 芯片编号标识（编辑时展示） -->
      <div
        v-if="camera?.cameraId"
        class="flex items-center justify-between rounded-xl border border-border/70 bg-muted/30 px-3.5 py-2 text-xs"
      >
        <span class="text-muted-foreground">{{
          $t('resource.camera.cameraId')
        }}</span>
        <div
          class="flex items-center gap-1.5 font-mono text-foreground font-medium"
        >
          <span>{{ camera.cameraId }}</span>
          <Button
            type="link"
            size="small"
            class="h-auto p-0 text-xs text-emerald-500"
            @click="copyToClipboard(camera.cameraId)"
          >
            {{ $t('resource.camera.copyId') }}
          </Button>
        </div>
      </div>

      <!-- 厂商快捷预设选择胶囊 -->
      <div>
        <label class="mb-1.5 block text-xs font-medium text-foreground">
          {{ $t('resource.camera.vendorPreset') }}
        </label>
        <RadioGroup
          :value="formState.vendor"
          size="small"
          class="w-full flex flex-wrap gap-1.5"
          @change="(e) => handleVendorChange(e.target.value)"
        >
          <RadioButton value="custom">
            {{ $t('resource.camera.vendorCustom') }}
          </RadioButton>
          <RadioButton value="hikvision">
            {{ $t('resource.camera.vendorHikvision') }}
          </RadioButton>
          <RadioButton value="dahua">
            {{ $t('resource.camera.vendorDahua') }}
          </RadioButton>
          <RadioButton value="uniview">
            {{ $t('resource.camera.vendorUniview') }}
          </RadioButton>
          <RadioButton value="tplink">
            {{ $t('resource.camera.vendorTplink') }}
          </RadioButton>
        </RadioGroup>
      </div>

      <!-- 基本信息：名称与网络配置 -->
      <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div>
          <label class="mb-1 block text-xs font-medium text-foreground">
            {{ $t('resource.camera.name') }}
            <span class="text-destructive">*</span>
          </label>
          <Input
            v-model:value="formState.name"
            :placeholder="$t('resource.camera.nameRequired')"
            size="middle"
          />
        </div>

        <!-- 当选择厂商模板时展示快捷参数 -->
        <template v-if="formState.vendor !== 'custom'">
          <div>
            <label class="mb-1 block text-xs font-medium text-foreground">
              {{ $t('resource.camera.ip') }}
              <span class="text-destructive">*</span>
            </label>
            <Input
              v-model:value="formState.ip"
              :placeholder="$t('resource.camera.ipPlaceholder')"
              @change="computeVendorUrls"
            />
          </div>

          <div>
            <label class="mb-1 block text-xs font-medium text-foreground">
              {{ $t('resource.camera.port') }}
            </label>
            <InputNumber
              v-model:value="formState.port"
              class="w-full"
              :min="1"
              :max="65535"
              @change="computeVendorUrls"
            />
          </div>

          <div>
            <label class="mb-1 block text-xs font-medium text-foreground">
              {{ $t('resource.camera.channel') }}
            </label>
            <InputNumber
              v-model:value="formState.channel"
              class="w-full"
              :min="1"
              :max="64"
              @change="computeVendorUrls"
            />
          </div>
        </template>

        <div>
          <label class="mb-1 block text-xs font-medium text-foreground">
            {{ $t('resource.camera.username') }}
          </label>
          <Input v-model:value="formState.username" placeholder="admin" />
        </div>

        <div>
          <label class="mb-1 block text-xs font-medium text-foreground">
            {{ $t('resource.camera.password') }}
          </label>
          <Input
            v-model:value="formState.password"
            placeholder="例如 123456 / admin123"
          />
        </div>
      </div>

      <!-- 主码流 RTSP 地址 -->
      <div>
        <div class="mb-1 flex items-center justify-between">
          <label class="text-xs font-medium text-foreground">
            {{ $t('resource.camera.address') }}
            <span class="text-destructive">*</span>
          </label>
          <span class="text-[11px] text-muted-foreground">
            {{ $t('resource.camera.addressPlaceholder') }}
          </span>
        </div>
        <Input
          v-model:value="formState.address"
          placeholder="rtsp://192.168.1.64:554/live/ch0"
          @change="handleAddressChange"
        />
      </div>

      <!-- 子码流 RTSP 地址 -->
      <div>
        <div class="mb-1 flex items-center justify-between">
          <label class="text-xs font-medium text-foreground">
            {{ $t('resource.camera.subRtspUrl') }}
          </label>
          <span class="text-[11px] text-muted-foreground">
            {{ $t('resource.camera.subRtspUrlPlaceholder') }}
          </span>
        </div>
        <Input
          v-model:value="formState.subRtspUrl"
          placeholder="rtsp://192.168.1.64:554/live/ch1 (可选)"
        />
      </div>

      <!-- 备注 -->
      <div>
        <label class="mb-1 block text-xs font-medium text-foreground">
          {{ $t('resource.camera.remark') }}
        </label>
        <Textarea
          v-model:value="formState.remark"
          :rows="2"
          placeholder="例如：东门入口机位"
        />
      </div>

      <!-- 最终完整 URL 预览与测试连接操作 -->
      <div
        class="rounded-xl border border-border/80 bg-muted/30 p-3 text-xs transition-colors"
      >
        <div
          class="flex items-center justify-between gap-2 border-b border-border/60 pb-2"
        >
          <span
            class="font-medium text-muted-foreground flex items-center gap-1.5"
          >
            <IconifyIcon icon="lucide:link" class="text-xs" />
            <span>{{ $t('resource.camera.finalUrl') }}</span>
          </span>
          <div class="flex items-center gap-2">
            <Button
              v-if="camera?.id"
              size="small"
              type="default"
              class="text-xs"
              @click="handleVerifyStream"
            >
              <template #icon>
                <IconifyIcon
                  icon="lucide:tv"
                  class="text-xs text-emerald-500"
                />
              </template>
              {{ $t('resource.camera.verifyStream') }}
            </Button>
            <Button
              type="primary"
              ghost
              size="small"
              class="text-xs text-emerald-500 border-emerald-500/50 hover:bg-emerald-500/10"
              :loading="probing"
              @click="handleProbe"
            >
              <template #icon>
                <IconifyIcon icon="lucide:activity" class="text-xs" />
              </template>
              {{
                probing
                  ? $t('resource.camera.probing')
                  : $t('resource.camera.probe')
              }}
            </Button>
          </div>
        </div>
        <div
          class="break-all pt-2 font-mono text-[11px] text-foreground/90 select-all"
        >
          {{ finalMainUrl || '-' }}
        </div>
      </div>

      <!-- 结构化链路握手诊断报告 -->
      <div
        v-if="probeResult"
        class="rounded-xl border p-3.5 text-xs transition-all"
        :class="
          probeResult.status === 'success'
            ? 'border-emerald-500/30 bg-emerald-950/10 dark:bg-emerald-950/20'
            : 'border-destructive/30 bg-destructive/10'
        "
      >
        <div class="flex items-center justify-between mb-2">
          <div class="flex items-center gap-2 font-semibold">
            <IconifyIcon
              :icon="
                probeResult.status === 'success'
                  ? 'lucide:check-circle-2'
                  : 'lucide:alert-circle'
              "
              :class="
                probeResult.status === 'success'
                  ? 'text-emerald-500'
                  : 'text-destructive'
              "
              class="text-base"
            />
            <span
              :class="
                probeResult.status === 'success'
                  ? 'text-emerald-600 dark:text-emerald-400'
                  : 'text-destructive'
              "
            >
              {{
                probeResult.status === 'success'
                  ? $t('resource.camera.probeSuccessDesc')
                  : $t('resource.camera.probeFailedDesc')
              }}
            </span>
          </div>

          <span class="font-mono text-muted-foreground text-[11px]">
            {{ $t('resource.camera.elapsed') }}:
            <strong class="text-foreground">{{ probeResult.elapsedMs }}ms</strong>
          </span>
        </div>

        <div
          v-if="probeResult.status === 'failed'"
          class="space-y-1 text-muted-foreground"
        >
          <div class="flex items-center gap-1.5 text-destructive font-medium">
            <span>{{ failureCodeText(probeResult.failureCode) }}</span>
          </div>
          <p class="text-[11px] text-muted-foreground/80 mt-1">
            请检查 IP
            地址、端口是否开放、用户名与密码是否正确，以及局域网防火墙规则。
          </p>
        </div>

        <div v-else class="space-y-2">
          <div
            class="grid grid-cols-2 md:grid-cols-4 gap-2 pt-1 font-mono text-xs"
          >
            <div class="rounded-lg bg-card/60 border border-border/50 p-2">
              <span class="text-[10px] text-muted-foreground block">{{
                $t('resource.camera.selectedTransport')
              }}</span>
              <span class="font-bold text-foreground uppercase">{{
                probeResult.selectedTransport
              }}</span>
            </div>
            <div class="rounded-lg bg-card/60 border border-border/50 p-2">
              <span class="text-[10px] text-muted-foreground block">{{
                $t('resource.camera.codec')
              }}</span>
              <span class="font-bold text-emerald-500 uppercase">{{
                probeResult.codec || 'H.264'
              }}</span>
            </div>
            <div class="rounded-lg bg-card/60 border border-border/50 p-2">
              <span class="text-[10px] text-muted-foreground block">{{
                $t('resource.camera.media')
              }}</span>
              <span class="font-bold text-foreground">{{ probeResult.width }}×{{ probeResult.height }}</span>
            </div>
            <div class="rounded-lg bg-card/60 border border-border/50 p-2">
              <span class="text-[10px] text-muted-foreground block">{{
                $t('resource.camera.fps')
              }}</span>
              <span class="font-bold text-foreground">{{ probeResult.fps }} fps</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </Modal>
</template>
