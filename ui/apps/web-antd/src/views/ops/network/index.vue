<script lang="ts" setup>
import { computed, onMounted, onUnmounted, ref } from 'vue';

import { Page } from '@vben/common-ui';
import { Check, IconifyIcon, Plus, RotateCw, X } from '@vben/icons';
import { $t } from '@vben/locales';

import {
  Alert,
  Badge,
  Button,
  Divider,
  Drawer,
  Empty,
  Form,
  Input,
  InputNumber,
  message,
  Popconfirm,
  Spin,
  Switch,
  Tag,
  Tooltip,
} from 'ant-design-vue';
import dayjs from 'dayjs';

import {
  applyInterfaceApi,
  cancelNetworkTransactionApi,
  confirmNetworkTransactionApi,
  factoryResetInterfaceApi,
  getNetworkOverviewApi,
  NETWORK_MODES,
  switchNetworkModeApi,
  type NetworkApi,
} from '#/api/core/network';

const loading = ref(false);
const submitting = ref(false);
const overview = ref<NetworkApi.NetworkOverview | null>(null);

// 120s 事务倒计时
const remainingSeconds = ref(0);
let timer: ReturnType<typeof setInterval> | null = null;

// 工作模式切换抽屉
const modeDrawerVisible = ref(false);
const modeSubmitting = ref(false);
const targetMode = ref<NetworkApi.NetworkMode>(NETWORK_MODES.ActiveBackup);

interface ModeFormState {
  slaveIds: string[];
  primarySlaveId: string;
  ipv4: NetworkApi.ApplyInterfaceParams;
  gateway: {
    downstreamInterfaceId: string;
    poolStart: string;
    poolEnd: string;
    prefix: number;
    leaseDurationSeconds: number;
    ipForward: boolean;
  };
}

const modeForm = ref<ModeFormState>({
  slaveIds: [],
  primarySlaveId: '',
  ipv4: {
    mode: 'dhcp',
    primary: true,
    dnsServers: [],
  },
  gateway: {
    downstreamInterfaceId: '',
    poolStart: '',
    poolEnd: '',
    prefix: 24,
    leaseDurationSeconds: 3600,
    ipForward: false,
  },
});

// 当前可作 slave 的物理网卡：可写、未被 bond 占用（isBond/masterId 为空）
const eligibleSlaves = computed(() =>
  (overview.value?.interfaces ?? []).filter(
    (i) => i.writable && !i.isBond && !i.masterId,
  ),
);

// 当前可作下行网关的接口：静态 IPv4、可写、非 bond、非 slave、非 primary
const eligibleGatewayIfaces = computed(() =>
  (overview.value?.interfaces ?? []).filter(
    (i) =>
      i.writable &&
      !i.isBond &&
      !i.masterId &&
      !i.isPrimary &&
      i.ipv4.mode === 'static' &&
      i.ipv4.address &&
      i.ipv4.prefix,
  ),
);

const activeBackupSupported = computed(() =>
  (overview.value?.capabilities.supportedModes ?? []).includes(
    NETWORK_MODES.ActiveBackup,
  ),
);

const gatewaySupported = computed(() =>
  (overview.value?.capabilities.supportedModes ?? []).includes(
    NETWORK_MODES.Gateway,
  ),
);

const canEnterActiveBackup = computed(
  () => activeBackupSupported.value && eligibleSlaves.value.length >= 2,
);

const canEnterGateway = computed(
  () => gatewaySupported.value && eligibleGatewayIfaces.value.length >= 1,
);

function deriveDefaultPool(ip?: string | null): { start: string; end: string } {
  if (!ip) return { start: '', end: '' };
  const parts = ip.split('.');
  if (parts.length === 4) {
    return {
      start: `${parts[0]}.${parts[1]}.${parts[2]}.100`,
      end: `${parts[0]}.${parts[1]}.${parts[2]}.200`,
    };
  }
  return { start: '', end: '' };
}

function formatDateTime(timeStr?: string | null): string {
  if (!timeStr) return '-';
  const d = dayjs(timeStr);
  return d.isValid() ? d.format('YYYY-MM-DD HH:mm:ss') : timeStr;
}

function openModeDrawer(mode: NetworkApi.NetworkMode = NETWORK_MODES.ActiveBackup) {
  targetMode.value = mode;
  if (mode === NETWORK_MODES.ActiveBackup) {
    const candidates = eligibleSlaves.value.slice(0, 2);
    modeForm.value.slaveIds = candidates.map((i) => i.id);
    modeForm.value.primarySlaveId = candidates[0]?.id ?? '';
    modeForm.value.ipv4 = {
      mode: 'dhcp',
      primary: true,
      dnsServers: [],
    };
  } else if (mode === NETWORK_MODES.Gateway) {
    const candidate = eligibleGatewayIfaces.value[0];
    const ifacePrefix = candidate?.ipv4.prefix ?? 24;
    const pool = deriveDefaultPool(candidate?.ipv4.address);
    modeForm.value.gateway = {
      downstreamInterfaceId: candidate?.id ?? '',
      poolStart: pool.start,
      poolEnd: pool.end,
      prefix: ifacePrefix,
      leaseDurationSeconds: 3600,
      ipForward: false,
    };
  }
  modeDrawerVisible.value = true;
}

function onGatewayIfaceChange(ifaceId: string) {
  const iface = eligibleGatewayIfaces.value.find((i) => i.id === ifaceId);
  if (!iface || !iface.ipv4.address || !iface.ipv4.prefix) return;
  modeForm.value.gateway.prefix = iface.ipv4.prefix;
  const pool = deriveDefaultPool(iface.ipv4.address);
  modeForm.value.gateway.poolStart = pool.start;
  modeForm.value.gateway.poolEnd = pool.end;
}

function toggleSlave(ifaceId: string) {
  const idx = modeForm.value.slaveIds.indexOf(ifaceId);
  if (idx !== -1) {
    modeForm.value.slaveIds.splice(idx, 1);
    if (modeForm.value.primarySlaveId === ifaceId) {
      modeForm.value.primarySlaveId = modeForm.value.slaveIds[0] ?? '';
    }
  } else if (modeForm.value.slaveIds.length < 2) {
    modeForm.value.slaveIds.push(ifaceId);
    if (!modeForm.value.primarySlaveId) {
      modeForm.value.primarySlaveId = ifaceId;
    }
  } else {
    message.warning($t('ops.network.modeSlaveLimitTip'));
  }
}

async function handleModeSwitch() {
  modeSubmitting.value = true;
  try {
    const params: NetworkApi.SwitchModeParams = { mode: targetMode.value };
    if (targetMode.value === NETWORK_MODES.ActiveBackup && modeForm.value.slaveIds.length === 2) {
      const ipv4: NetworkApi.ApplyInterfaceParams = {
        mode: modeForm.value.ipv4.mode,
        primary: modeForm.value.ipv4.primary,
      };
      if (modeForm.value.ipv4.mode === 'static') {
        ipv4.address = modeForm.value.ipv4.address;
        ipv4.prefix = modeForm.value.ipv4.prefix;
        if (modeForm.value.ipv4.primary) {
          ipv4.gateway = modeForm.value.ipv4.gateway;
          ipv4.dnsServers = (modeForm.value.ipv4.dnsServers ?? []).filter(
            (s) => s.trim() !== '',
          );
        }
      }
      params.bond = {
        slaveIds: modeForm.value.slaveIds,
        primarySlaveId: modeForm.value.primarySlaveId,
        ipv4,
      };
    } else if (targetMode.value === NETWORK_MODES.Gateway) {
      params.gateway = {
        downstreamInterfaceId: modeForm.value.gateway.downstreamInterfaceId,
        poolStart: modeForm.value.gateway.poolStart,
        poolEnd: modeForm.value.gateway.poolEnd,
        prefix: modeForm.value.gateway.prefix,
        leaseDurationSeconds: modeForm.value.gateway.leaseDurationSeconds,
        ipForward: modeForm.value.gateway.ipForward,
      };
    }
    await switchNetworkModeApi(params);
    message.success($t('ops.network.modeSwitchSuccess'));
    modeDrawerVisible.value = false;
    await loadData();
  } catch (error) {
    console.error('Failed to switch network mode:', error);
  } finally {
    modeSubmitting.value = false;
  }
}

async function handleModeExit() {
  modeSubmitting.value = true;
  try {
    await switchNetworkModeApi({ mode: NETWORK_MODES.MultiAddress });
    message.success($t('ops.network.modeSwitchSuccess'));
    await loadData();
  } catch (error) {
    console.error('Failed to exit active-backup mode:', error);
  } finally {
    modeSubmitting.value = false;
  }
}

// 抽屉与表单
const drawerVisible = ref(false);
const currentIface = ref<NetworkApi.InterfaceInfo | null>(null);
const formModel = ref<{
  mode: NetworkApi.IPMode;
  primary: boolean;
  address?: string;
  prefix?: number;
  gateway?: string;
  dnsServers: string[];
}>({
  mode: 'dhcp',
  primary: false,
  dnsServers: [],
});

const recommendedDNS = [
  { ip: '223.5.5.5', name: 'Aliyun' },
  { ip: '119.29.29.29', name: 'DNSPod' },
  { ip: '8.8.8.8', name: 'Google' },
  { ip: '1.1.1.1', name: 'Cloudflare' },
];

const isPrimary = computed(() => formModel.value.primary);
const isStatic = computed(() => formModel.value.mode === 'static');

// 根据 CIDR 前缀自动计算子网掩码
const calculatedSubnetMask = computed(() => {
  const prefix = formModel.value.prefix;
  if (!prefix || prefix < 1 || prefix > 32) return '-';
  const mask = (0xffffffff << (32 - prefix)) >>> 0;
  return [
    (mask >>> 24) & 255,
    (mask >>> 16) & 255,
    (mask >>> 8) & 255,
    mask & 255,
  ].join('.');
});

// 计算在线网口数量
const upLinkCount = computed(() => {
  if (!overview.value?.interfaces) return 0;
  return overview.value.interfaces.filter((i) => i.linkStatus === 'up').length;
});

async function loadData() {
  loading.value = true;
  try {
    const data = await getNetworkOverviewApi();
    overview.value = data;

    if (data.pendingTransaction) {
      remainingSeconds.value = data.pendingTransaction.remainingSeconds;
      startCountdown();
    } else {
      stopCountdown();
    }
  } catch (error) {
    console.error('Failed to load network overview:', error);
  } finally {
    loading.value = false;
  }
}

function startCountdown() {
  if (timer) clearInterval(timer);
  timer = setInterval(() => {
    if (remainingSeconds.value > 0) {
      remainingSeconds.value--;
    } else {
      stopCountdown();
      loadData();
    }
  }, 1000);
}

function stopCountdown() {
  if (timer) {
    clearInterval(timer);
    timer = null;
  }
  remainingSeconds.value = 0;
}

function openEdit(iface: NetworkApi.InterfaceInfo) {
  currentIface.value = iface;
  formModel.value = {
    mode: iface.ipv4?.mode === 'static' ? 'static' : 'dhcp',
    primary: iface.isPrimary ?? false,
    address: iface.ipv4?.address ?? undefined,
    prefix: iface.ipv4?.prefix ?? 24,
    gateway: iface.ipv4?.gateway ?? undefined,
    dnsServers: iface.ipv4?.dnsServers ? [...iface.ipv4.dnsServers] : [],
  };
  drawerVisible.value = true;
}

function selectIPMode(targetMode: 'dhcp' | 'static') {
  formModel.value.mode = targetMode;
  if (targetMode === 'static' && !formModel.value.prefix) {
    formModel.value.prefix = 24;
  }
}

function addDNSServer() {
  if (formModel.value.dnsServers.length < 3) {
    formModel.value.dnsServers.push('');
  }
}

function removeDNSServer(index: number) {
  formModel.value.dnsServers.splice(index, 1);
}

function quickFillDNS(ip: string) {
  if (formModel.value.dnsServers.includes(ip)) return;
  if (formModel.value.dnsServers.length < 3) {
    formModel.value.dnsServers.push(ip);
  } else {
    message.warning($t('ops.network.dnsLimitTip'));
  }
}

async function handleApply() {
  if (!currentIface.value) return;
  submitting.value = true;
  try {
    const params: NetworkApi.ApplyInterfaceParams = {
      mode: formModel.value.mode,
      primary: formModel.value.primary,
    };
    if (formModel.value.mode === 'static') {
      params.address = formModel.value.address;
      params.prefix = formModel.value.prefix;
      if (formModel.value.primary) {
        params.gateway = formModel.value.gateway;
        params.dnsServers = formModel.value.dnsServers.filter(
          (s) => s.trim() !== '',
        );
      }
    }
    await applyInterfaceApi(currentIface.value.id, params);
    message.success($t('ops.network.applySuccess'));
    drawerVisible.value = false;
    await loadData();
  } catch (error) {
    console.error('Failed to apply interface config:', error);
  } finally {
    submitting.value = false;
  }
}

async function handleConfirm() {
  if (!overview.value?.pendingTransaction) return;
  submitting.value = true;
  try {
    await confirmNetworkTransactionApi(overview.value.pendingTransaction.id);
    message.success($t('ops.network.confirmSuccess'));
    await loadData();
  } catch (error) {
    console.error('Failed to confirm transaction:', error);
  } finally {
    submitting.value = false;
  }
}

async function handleCancel() {
  if (!overview.value?.pendingTransaction) return;
  submitting.value = true;
  try {
    await cancelNetworkTransactionApi(overview.value.pendingTransaction.id);
    message.success($t('ops.network.cancelSuccess'));
    await loadData();
  } catch (error) {
    console.error('Failed to cancel transaction:', error);
  } finally {
    submitting.value = false;
  }
}

async function handleFactoryReset(iface: NetworkApi.InterfaceInfo) {
  loading.value = true;
  try {
    await factoryResetInterfaceApi(iface.id);
    message.success($t('ops.network.resetSuccess'));
    await loadData();
  } catch (error) {
    console.error('Failed to reset interface:', error);
  } finally {
    loading.value = false;
  }
}

function copyToClipboard(
  text: string | null | undefined,
  type: 'ip' | 'mac' = 'ip',
) {
  if (!text) return;
  navigator.clipboard.writeText(text);
  message.info(
    type === 'ip' ? $t('ops.network.ipCopied') : $t('ops.network.macCopied'),
  );
}

onMounted(() => {
  loadData();
});

onUnmounted(() => {
  stopCountdown();
});
</script>

<template>
  <Page auto-content-height>
    <div class="space-y-4 pb-6">
      <!-- 120s 事务防失联确认卡片 (Active Pending Banner) -->
      <div
        v-if="overview?.pendingTransaction"
        class="bg-amber-500/10 border-amber-500/50 dark:bg-amber-500/15 rounded-xl border p-5 shadow-xs transition-all duration-200"
      >
        <div
          class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between"
        >
          <div class="space-y-1.5">
            <div class="flex items-center gap-2">
              <span class="relative flex size-3">
                <span
                  class="animate-ping absolute inline-flex h-full w-full rounded-full bg-amber-400 opacity-75"
                ></span>
                <span
                  class="relative inline-flex rounded-full size-3 bg-amber-500"
                ></span>
              </span>
              <span
                class="text-base font-bold text-amber-600 dark:text-amber-400"
              >
                {{ $t('ops.network.pendingCardTitle') }}
              </span>
            </div>
            <p class="text-xs text-muted-foreground leading-relaxed">
              {{ $t('ops.network.pendingTip') }}
            </p>
            <div
              v-if="overview.pendingTransaction.reconnectAddresses?.length"
              class="flex flex-wrap items-center gap-2 pt-1 text-xs"
            >
              <span class="font-medium text-amber-700 dark:text-amber-300">
                {{ $t('ops.network.reconnectAlert') }}
              </span>
              <div class="flex flex-wrap gap-1.5">
                <Tag
                  v-for="item in overview.pendingTransaction.reconnectAddresses"
                  :key="item.interfaceId"
                  color="warning"
                  class="cursor-pointer font-mono font-medium hover:opacity-80"
                  @click="copyToClipboard(item.address, 'ip')"
                >
                  <IconifyIcon
                    icon="lucide:copy"
                    class="size-3 inline mr-1 opacity-70"
                  />
                  {{ item.address }}/{{ item.prefix }} ({{ item.interfaceId }})
                </Tag>
              </div>
            </div>
          </div>

          <div class="flex items-center gap-4 shrink-0">
            <div class="text-right">
              <div class="text-xs text-muted-foreground font-medium">
                {{ $t('ops.network.remainingSeconds') }}
              </div>
              <div
                class="font-mono text-2xl font-black text-amber-600 dark:text-amber-400"
              >
                {{ remainingSeconds }}s
              </div>
            </div>
            <div class="flex items-center gap-2">
              <Button
                v-access:code="['ops:network:confirm']"
                type="primary"
                :loading="submitting"
                class="bg-amber-600 hover:bg-amber-500 border-none shadow-xs"
                @click="handleConfirm"
              >
                <template #icon>
                  <Check class="size-4 inline mr-1" />
                </template>
                {{ $t('ops.network.confirmChange') }}
              </Button>
              <Button
                v-access:code="['ops:network:cancel']"
                danger
                :loading="submitting"
                @click="handleCancel"
              >
                <template #icon>
                  <X class="size-4 inline mr-1" />
                </template>
                {{ $t('ops.network.cancelChange') }}
              </Button>
            </div>
          </div>
        </div>
      </div>

      <!-- 网络工作模式卡片 (Network Working Mode Panel) -->
      <div
        class="bg-card text-card-foreground rounded-xl border p-5 shadow-xs transition-all duration-200"
      >
        <div class="flex items-center justify-between mb-4">
          <div class="flex items-center gap-2">
            <div
              class="bg-violet-500/10 text-violet-500 dark:bg-violet-500/20 flex size-8 items-center justify-center rounded-lg"
            >
              <IconifyIcon icon="lucide:layers" class="size-4.5" />
            </div>
            <div>
              <span class="text-foreground font-semibold text-sm block">
                {{ $t('ops.network.modeTitle') }}
              </span>
              <span class="text-muted-foreground text-xs">
                {{ $t('ops.network.modeTip') }}
              </span>
            </div>
          </div>
          <Tag
            color="processing"
            class="font-mono font-semibold border-0 uppercase"
          >
            {{ overview?.mode || NETWORK_MODES.MultiAddress }}
          </Tag>
        </div>

        <!-- 当前拓扑（active-backup 时展示 bond 与 slave 从属关系） -->
        <div v-if="overview?.bond" class="mb-4">
          <div class="text-muted-foreground text-xs mb-2">
            {{ $t('ops.network.modeTopology') }}
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <Tag color="purple" class="border-0 font-mono font-semibold">
              <IconifyIcon icon="lucide:layers" class="size-3 inline mr-1" />
              {{ overview.bond.bondInterfaceId }} (miimon={{
                overview.bond.miimon
              }})
            </Tag>
            <IconifyIcon
              icon="lucide:arrow-right"
              class="size-3.5 text-muted-foreground"
            />
            <template v-for="sid in overview.bond.slaveIds" :key="sid">
              <Tag
                :color="
                  overview.bond.activeSlaveId === sid ? 'success' : 'default'
                "
                class="border-0 font-mono"
              >
                <span
                  v-if="overview.bond.activeSlaveId === sid"
                  class="inline-block size-1.5 rounded-full bg-emerald-500 mr-1"
                ></span>
                {{ sid }}
                <span
                  v-if="overview.bond.primarySlaveId === sid"
                  class="text-[10px] opacity-70"
                >
                  (primary)
                </span>
              </Tag>
            </template>
          </div>
        </div>

        <!-- 边缘网关状态（gateway 模式时展示下行接口、地址池、ip_forward 与冲突告警） -->
        <div v-if="overview?.gateway" class="mb-4">
          <Alert
            v-if="overview.gateway.conflictDetected"
            type="error"
            show-icon
            :message="$t('ops.network.gatewayConflictWarning')"
            class="!text-xs mb-3"
          />
          <div class="text-muted-foreground text-xs mb-2">
            {{ $t('ops.network.modeGatewayTitle') }}
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <Tag color="blue" class="border-0 font-mono font-semibold">
              <IconifyIcon icon="lucide:router" class="size-3 inline mr-1" />
              LAN: {{ overview.gateway.downstreamInterfaceId }}
            </Tag>
            <Tag color="cyan" class="border-0 font-mono">
              Pool: {{ overview.gateway.poolStart }} ~ {{ overview.gateway.poolEnd }} /{{ overview.gateway.prefix }}
            </Tag>
            <Tag color="default" class="border-0 font-mono text-xs">
              Lease: {{ overview.gateway.leaseDurationSeconds }}s
            </Tag>
            <Tag :color="overview.gateway.ipForward ? 'green' : 'default'" class="border-0 font-mono text-xs">
              ip_forward: {{ overview.gateway.ipForward ? 'ON' : 'OFF' }}
            </Tag>
          </div>
        </div>

        <div class="flex items-center justify-between">
          <div class="text-xs text-muted-foreground">
            {{ $t('ops.network.modeSupported') }}:
            <template
              v-for="m in overview?.capabilities.supportedModes ?? []"
              :key="m"
            >
              <Tag class="ml-1 border-0 font-mono">{{ m }}</Tag>
            </template>
          </div>
          <div class="flex items-center gap-2">
            <!-- Active-Backup 按钮 -->
            <template v-if="overview?.mode === NETWORK_MODES.ActiveBackup">
              <Popconfirm
                :title="$t('ops.network.modeExitConfirm')"
                @confirm="handleModeExit"
              >
                <Button
                  v-access:code="['ops:network:mode']"
                  danger
                  :disabled="!!overview?.pendingTransaction"
                >
                  {{ $t('ops.network.modeExit') }}
                </Button>
              </Popconfirm>
            </template>
            <!-- Gateway 按钮 -->
            <template v-else-if="overview?.mode === NETWORK_MODES.Gateway">
              <Popconfirm
                :title="$t('ops.network.modeExitGatewayConfirm')"
                @confirm="handleModeExit"
              >
                <Button
                  v-access:code="['ops:network:mode']"
                  danger
                  :disabled="!!overview?.pendingTransaction"
                >
                  {{ $t('ops.network.modeExitGateway') }}
                </Button>
              </Popconfirm>
            </template>
            <!-- Multi-Address 模式下提供切换入口 -->
            <template v-else>
              <Button
                v-if="activeBackupSupported"
                v-access:code="['ops:network:mode']"
                type="primary"
                ghost
                :disabled="!canEnterActiveBackup || !!overview?.pendingTransaction"
                @click="openModeDrawer(NETWORK_MODES.ActiveBackup)"
              >
                <IconifyIcon icon="lucide:layers" class="size-3.5 inline mr-1" />
                {{ $t('ops.network.modeEnter') }}
              </Button>

              <Button
                v-if="gatewaySupported"
                v-access:code="['ops:network:mode']"
                type="primary"
                ghost
                :disabled="!canEnterGateway || !!overview?.pendingTransaction"
                @click="openModeDrawer(NETWORK_MODES.Gateway)"
              >
                <IconifyIcon icon="lucide:router" class="size-3.5 inline mr-1" />
                {{ $t('ops.network.modeEnterGateway') }}
              </Button>
            </template>
          </div>
        </div>
      </div>

      <!-- 顶部四大系统指标看板 (Dashboard Overview Cards) -->
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <!-- 默认主出口 (WAN) -->
        <div
          class="bg-card text-card-foreground rounded-xl border p-5 shadow-xs transition-all duration-200"
        >
          <div class="flex items-center justify-between">
            <span class="text-muted-foreground text-sm font-medium">
              {{ $t('ops.network.primaryInterface') }}
            </span>
            <div
              class="bg-emerald-500/10 text-emerald-500 dark:bg-emerald-500/20 flex size-8 items-center justify-center rounded-lg"
            >
              <IconifyIcon icon="lucide:globe" class="size-4.5" />
            </div>
          </div>
          <div class="mt-3 flex items-baseline gap-2">
            <div
              class="font-mono text-xl font-bold tracking-tight text-foreground"
            >
              {{ overview?.primaryInterfaceId || $t('ops.network.none') }}
            </div>
            <Tag
              v-if="overview?.primaryInterfaceId"
              color="success"
              class="border-0 font-medium font-mono text-[11px]"
            >
              WAN
            </Tag>
          </div>
          <p class="text-muted-foreground mt-1 text-xs">
            {{ $t('ops.network.primaryInterfaceTip') }}
          </p>
        </div>

        <!-- 物理链路状态 -->
        <div
          class="bg-card text-card-foreground rounded-xl border p-5 shadow-xs transition-all duration-200"
        >
          <div class="flex items-center justify-between">
            <span class="text-muted-foreground text-sm font-medium">
              {{ $t('ops.network.linkOverview') }}
            </span>
            <div
              class="bg-primary/10 text-primary dark:bg-primary/20 flex size-8 items-center justify-center rounded-lg"
            >
              <IconifyIcon icon="lucide:cable" class="size-4.5" />
            </div>
          </div>
          <div class="mt-3 flex items-baseline gap-2">
            <div
              class="font-mono text-xl font-bold tracking-tight text-foreground"
            >
              {{ upLinkCount }} / {{ overview?.interfaces?.length ?? 0 }}
            </div>
            <span class="text-xs text-muted-foreground font-medium">
              {{ $t('ops.network.activeLinks') }}
            </span>
          </div>
          <p class="text-muted-foreground mt-1 text-xs">
            {{ $t('ops.network.linkOverviewTip') }}
          </p>
        </div>

        <!-- 系统 DNS -->
        <div
          class="bg-card text-card-foreground rounded-xl border p-5 shadow-xs transition-all duration-200"
        >
          <div class="flex items-center justify-between">
            <span class="text-muted-foreground text-sm font-medium">
              {{ $t('ops.network.systemDNS') }}
            </span>
            <div
              class="bg-sky-500/10 text-sky-500 dark:bg-sky-500/20 flex size-8 items-center justify-center rounded-lg"
            >
              <IconifyIcon icon="lucide:server" class="size-4.5" />
            </div>
          </div>
          <div class="mt-3 flex items-center gap-1.5 flex-wrap">
            <template v-if="overview?.systemDnsServers?.length">
              <Tag
                v-for="dns in overview.systemDnsServers"
                :key="dns"
                color="blue"
                class="border-0 font-mono text-xs font-semibold"
              >
                {{ dns }}
              </Tag>
            </template>
            <span v-else class="text-base font-bold text-muted-foreground">
              {{ $t('ops.network.none') }}
            </span>
          </div>
          <p class="text-muted-foreground mt-1 text-xs">
            {{ $t('ops.network.systemDNSTip') }}
          </p>
        </div>

        <!-- 运行平台与刷新 -->
        <div
          class="bg-card text-card-foreground rounded-xl border p-5 shadow-xs transition-all duration-200"
        >
          <div class="flex items-center justify-between">
            <span class="text-muted-foreground text-sm font-medium">
              {{ $t('ops.network.platform') }}
            </span>
            <div
              class="bg-amber-500/10 text-amber-500 dark:bg-amber-500/20 flex size-8 items-center justify-center rounded-lg"
            >
              <IconifyIcon icon="lucide:cpu" class="size-4.5" />
            </div>
          </div>
          <div class="mt-3 flex items-center justify-between">
            <Tag
              color="cyan"
              class="font-mono text-xs font-bold uppercase tracking-wide border-0 py-0.5 px-2.5"
            >
              {{ overview?.platform || 'LINUX' }}
            </Tag>
            <Button
              type="link"
              size="small"
              class="!p-0 !h-auto text-xs flex items-center gap-1"
              :loading="loading"
              @click="loadData"
            >
              <RotateCw class="size-3.5 inline mr-0.5" />
              {{ $t('ops.network.refresh') }}
            </Button>
          </div>
          <p class="text-muted-foreground mt-1 text-xs">
            {{ overview?.state || 'ready' }}
          </p>
        </div>
      </div>

      <!-- 主工作区：左侧网卡列表卡片 + 右侧指南与规格 -->
      <div class="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <!-- 左侧核心网卡网格 (占2列) -->
        <div class="lg:col-span-2 space-y-4">
          <!-- 拓扑与隔离提示条 -->
          <div
            class="bg-muted/40 text-muted-foreground rounded-xl border p-3.5 text-xs flex items-center gap-2 leading-relaxed"
          >
            <IconifyIcon
              icon="lucide:info"
              class="size-4.5 text-primary shrink-0"
            />
            <span>{{ $t('ops.network.topologyTip') }}</span>
          </div>

          <!-- 网卡卡片列表 -->
          <Spin :spinning="loading">
            <div
              v-if="!overview?.interfaces?.length"
              class="bg-card rounded-xl border p-12 text-center"
            >
              <Empty :description="$t('ops.network.none')" />
            </div>

            <div v-else class="grid grid-cols-1 gap-4 md:grid-cols-2">
              <div
                v-for="iface in overview.interfaces"
                :key="iface.id"
                class="bg-card text-card-foreground rounded-xl border p-5 shadow-xs transition-all duration-200 hover:shadow-md flex flex-col justify-between"
                :class="[
                  iface.isPrimary
                    ? 'border-primary/60 bg-primary/5 ring-1 ring-primary/20 dark:bg-primary/10'
                    : 'border-border',
                ]"
              >
                <div>
                  <!-- 卡片头部：网口名称、类型与 WAN/LAN 标签 -->
                  <div
                    class="flex items-start justify-between gap-2 border-b pb-3.5"
                  >
                    <div class="flex items-center gap-2.5">
                      <div
                        class="flex size-9 items-center justify-center rounded-lg shrink-0"
                        :class="[
                          iface.isPrimary
                            ? 'bg-primary text-primary-foreground'
                            : 'bg-muted text-muted-foreground',
                        ]"
                      >
                        <IconifyIcon
                          :icon="
                            iface.type === 'wifi'
                              ? 'lucide:wifi'
                              : 'lucide:network'
                          "
                          class="size-5"
                        />
                      </div>
                      <div>
                        <div
                          class="font-bold text-sm text-foreground flex items-center gap-1.5"
                        >
                          <span>{{ iface.displayName || iface.name }}</span>
                          <span class="font-mono text-xs text-muted-foreground"
                            >({{ iface.name }})</span
                          >
                        </div>
                        <div
                          class="text-[11px] text-muted-foreground uppercase font-mono"
                        >
                          {{ iface.type }}
                        </div>
                      </div>
                    </div>

                    <div class="flex items-center gap-1.5 shrink-0">
                      <Tag
                        v-if="iface.isPrimary"
                        color="success"
                        class="border-0 font-semibold text-xs"
                      >
                        {{ $t('ops.network.primaryTag') }}
                      </Tag>
                      <Tag v-else color="default" class="border-0 text-xs">
                        {{ $t('ops.network.lanBadge') }}
                      </Tag>
                    </div>
                  </div>

                  <!-- 链路与工作模式 -->
                  <div
                    class="flex items-center justify-between py-2.5 border-b border-border/50 text-xs"
                  >
                    <div class="flex items-center gap-2">
                      <Badge
                        :status="
                          iface.linkStatus === 'up' ? 'success' : 'default'
                        "
                        :text="
                          iface.linkStatus === 'up'
                            ? $t('ops.network.linkUp')
                            : $t('ops.network.linkDown')
                        "
                      />
                    </div>
                    <Tag
                      :color="
                        iface.ipv4?.mode === 'static' ? 'processing' : 'default'
                      "
                      class="font-mono font-medium uppercase border-0 text-[11px]"
                    >
                      {{ iface.ipv4?.mode || 'DHCP' }}
                    </Tag>
                  </div>

                  <!-- IP 地址核心大展示 -->
                  <div class="py-3">
                    <div class="text-[11px] text-muted-foreground mb-1">
                      {{ $t('ops.network.ipAddress') }}
                    </div>
                    <div
                      v-if="iface.ipv4?.address"
                      class="group flex items-center justify-between cursor-pointer rounded-lg bg-muted/40 p-2 border hover:border-primary/50 transition-colors"
                      @click="copyToClipboard(iface.ipv4.address, 'ip')"
                    >
                      <span
                        class="font-mono font-bold text-base text-foreground group-hover:text-primary transition-colors"
                      >
                        {{ iface.ipv4.address }}
                        <span class="text-muted-foreground font-normal text-xs"
                          >/{{ iface.ipv4.prefix || 24 }}</span
                        >
                      </span>
                      <Tooltip :title="$t('ops.network.ipCopied')">
                        <IconifyIcon
                          icon="lucide:copy"
                          class="size-3.5 text-muted-foreground group-hover:text-primary transition-colors"
                        />
                      </Tooltip>
                    </div>
                    <div
                      v-else
                      class="text-sm font-mono text-muted-foreground py-1"
                    >
                      -
                    </div>
                  </div>

                  <!-- 关键参数指标行 -->
                  <div class="space-y-2 text-xs">
                    <div
                      class="flex justify-between py-1 border-b border-border/50"
                    >
                      <span class="text-muted-foreground">{{
                        $t('ops.network.subnetMask')
                      }}</span>
                      <span class="font-mono font-medium text-foreground">
                        {{ iface.ipv4?.subnetMask || '-' }}
                      </span>
                    </div>

                    <div
                      class="flex justify-between py-1 border-b border-border/50"
                    >
                      <span class="text-muted-foreground">{{
                        $t('ops.network.gateway')
                      }}</span>
                      <span class="font-mono font-medium text-foreground">
                        {{ iface.isPrimary ? iface.ipv4?.gateway || '-' : '-' }}
                      </span>
                    </div>

                    <div
                      class="flex justify-between py-1 border-b border-border/50"
                    >
                      <span class="text-muted-foreground">{{
                        $t('ops.network.mac')
                      }}</span>
                      <span
                        class="font-mono text-muted-foreground hover:text-primary cursor-pointer transition-colors"
                        @click="copyToClipboard(iface.mac, 'mac')"
                      >
                        {{ iface.mac || '-' }}
                      </span>
                    </div>

                    <div
                      v-if="iface.isPrimary && iface.ipv4?.dnsServers?.length"
                      class="py-1"
                    >
                      <div class="text-muted-foreground mb-1">
                        {{ $t('ops.network.dnsServers') }}
                      </div>
                      <div class="flex flex-wrap gap-1">
                        <Tag
                          v-for="dns in iface.ipv4.dnsServers"
                          :key="dns"
                          class="font-mono text-[11px] border-0 bg-muted text-muted-foreground"
                        >
                          {{ dns }}
                        </Tag>
                      </div>
                    </div>
                  </div>
                </div>

                <!-- 底部操作区 -->
                <div
                  class="mt-4 pt-3 border-t flex items-center justify-between"
                >
                  <Button
                    v-access:code="['ops:network:edit']"
                    type="primary"
                    size="small"
                    ghost
                    :disabled="!iface.writable"
                    class="flex items-center gap-1"
                    @click="openEdit(iface)"
                  >
                    <IconifyIcon icon="lucide:settings-2" class="size-3.5" />
                    {{ $t('ops.network.configure') }}
                  </Button>

                  <Popconfirm
                    :title="$t('ops.network.factoryResetConfirm')"
                    @confirm="handleFactoryReset(iface)"
                  >
                    <Button
                      v-access:code="['ops:network:reset']"
                      type="text"
                      danger
                      size="small"
                      :disabled="!iface.writable"
                      class="flex items-center gap-1"
                    >
                      <IconifyIcon icon="lucide:rotate-ccw" class="size-3.5" />
                      {{ $t('ops.network.factoryReset') }}
                    </Button>
                  </Popconfirm>
                </div>
              </div>
            </div>

            <!-- DHCP 租约列表（网关模式生效时展示） -->
            <div
              v-if="overview?.mode === NETWORK_MODES.Gateway && overview?.gateway"
              class="mt-6 bg-card text-card-foreground rounded-xl border p-5 shadow-xs"
            >
              <div class="flex items-center justify-between mb-4">
                <div class="flex items-center gap-2">
                  <div
                    class="bg-blue-500/10 text-blue-500 dark:bg-blue-500/20 flex size-8 items-center justify-center rounded-lg"
                  >
                    <IconifyIcon icon="lucide:list" class="size-4.5" />
                  </div>
                  <div>
                    <span class="text-foreground font-semibold text-sm block">
                      {{ $t('ops.network.gatewayLeasesTitle') }}
                    </span>
                    <span class="text-muted-foreground text-xs">
                      {{ overview.gateway.leases.length }} leases active
                    </span>
                  </div>
                </div>
              </div>

              <div v-if="overview.gateway.leases.length === 0" class="py-6">
                <Empty :description="$t('ops.network.none')" />
              </div>
              <div v-else class="overflow-x-auto">
                <table class="w-full text-xs text-left border-collapse font-mono">
                  <thead>
                    <tr class="border-b border-border/60 text-muted-foreground">
                      <th class="py-2.5 px-3 font-semibold">{{ $t('ops.network.gatewayLeaseIP') }}</th>
                      <th class="py-2.5 px-3 font-semibold">{{ $t('ops.network.gatewayLeaseMAC') }}</th>
                      <th class="py-2.5 px-3 font-semibold">{{ $t('ops.network.gatewayLeaseHostname') }}</th>
                      <th class="py-2.5 px-3 font-semibold">{{ $t('ops.network.gatewayLeaseStartsAt') }}</th>
                      <th class="py-2.5 px-3 font-semibold">{{ $t('ops.network.gatewayLeaseExpiresAt') }}</th>
                      <th class="py-2.5 px-3 font-semibold">{{ $t('ops.network.gatewayLeaseRenewedAt') }}</th>
                    </tr>
                  </thead>
                  <tbody class="divide-y divide-border/40">
                    <tr
                      v-for="l in overview.gateway.leases"
                      :key="l.mac"
                      class="hover:bg-muted/30 transition-colors"
                    >
                      <td class="py-2.5 px-3 font-bold text-foreground">{{ l.ip }}</td>
                      <td class="py-2.5 px-3 text-muted-foreground">{{ l.mac }}</td>
                      <td class="py-2.5 px-3 text-foreground">{{ l.hostname || '-' }}</td>
                      <td class="py-2.5 px-3 text-muted-foreground">{{ formatDateTime(l.startsAt) }}</td>
                      <td class="py-2.5 px-3 text-muted-foreground">{{ formatDateTime(l.expiresAt) }}</td>
                      <td class="py-2.5 px-3 text-muted-foreground">{{ formatDateTime(l.lastRenewedAt) }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>
          </Spin>
        </div>

        <!-- 右侧使用指南与技术规格 (占1列) -->
        <div class="space-y-4">
          <!-- 工控多网隔离与使用指南 -->
          <div
            class="bg-card text-card-foreground rounded-xl border p-5 shadow-xs"
          >
            <div
              class="flex items-center gap-2 text-foreground font-semibold text-sm mb-4"
            >
              <IconifyIcon
                icon="lucide:book-open-check"
                class="size-4.5 text-primary"
              />
              <span>{{ $t('ops.network.guideTitle') }}</span>
            </div>
            <div
              class="space-y-3.5 text-xs text-muted-foreground leading-relaxed"
            >
              <p class="bg-muted/50 p-3 rounded-lg border">
                {{ $t('ops.network.guide1') }}
              </p>
              <p class="bg-muted/50 p-3 rounded-lg border">
                {{ $t('ops.network.guide2') }}
              </p>
              <p class="bg-muted/50 p-3 rounded-lg border">
                {{ $t('ops.network.guide3') }}
              </p>
            </div>
          </div>

          <!-- 网络技术规格与约束 -->
          <div
            class="bg-card text-card-foreground rounded-xl border p-5 shadow-xs"
          >
            <div
              class="flex items-center gap-2 text-foreground font-semibold text-sm mb-3"
            >
              <IconifyIcon
                icon="lucide:shield-check"
                class="size-4.5 text-sky-500"
              />
              <span>{{ $t('ops.network.specsTitle') }}</span>
            </div>
            <div class="space-y-2 text-xs text-muted-foreground">
              <div class="flex justify-between py-1 border-b border-border/50">
                <span>{{ $t('ops.network.specRouting') }}</span>
                <span class="font-medium text-foreground">{{
                  $t('ops.network.specRoutingValue')
                }}</span>
              </div>
              <div class="flex justify-between py-1 border-b border-border/50">
                <span>{{ $t('ops.network.specRollback') }}</span>
                <span class="font-medium text-foreground">{{
                  $t('ops.network.specRollbackValue')
                }}</span>
              </div>
              <div class="flex justify-between py-1">
                <span>{{ $t('ops.network.specIsolation') }}</span>
                <span class="font-medium text-foreground">{{
                  $t('ops.network.specIsolationValue')
                }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 侧边抽屉配置面板 (Drawer Configuration Panel) -->
    <Drawer
      v-model:open="drawerVisible"
      :title="`${$t('ops.network.configure')} - ${currentIface?.displayName || currentIface?.name}`"
      width="460px"
      :destroy-on-close="true"
    >
      <Form layout="vertical" class="space-y-5">
        <Alert
          type="warning"
          show-icon
          :message="$t('ops.network.disconnectWarning')"
          class="!text-xs"
        />

        <!-- 模式选择卡片区 -->
        <div>
          <label class="text-sm font-semibold text-foreground block mb-2.5">
            {{ $t('ops.network.ipMode') }}
          </label>
          <div class="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
            <!-- DHCP 模式卡片 -->
            <div
              class="cursor-pointer rounded-xl border-2 p-3.5 transition-all duration-150"
              :class="[
                formModel.mode === 'dhcp'
                  ? 'border-primary bg-primary/5 dark:bg-primary/10 shadow-xs'
                  : 'border-border hover:border-primary/50 hover:bg-muted/30',
              ]"
              @click="selectIPMode('dhcp')"
            >
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-2">
                  <div
                    class="flex size-6 items-center justify-center rounded-md"
                    :class="[
                      formModel.mode === 'dhcp'
                        ? 'bg-primary text-primary-foreground'
                        : 'bg-muted text-muted-foreground',
                    ]"
                  >
                    <IconifyIcon icon="lucide:radio" class="size-3.5" />
                  </div>
                  <span class="font-semibold text-xs">{{
                    $t('ops.network.modeDHCP')
                  }}</span>
                </div>
                <div
                  class="size-3.5 rounded-full border flex items-center justify-center"
                  :class="[
                    formModel.mode === 'dhcp'
                      ? 'border-primary bg-primary'
                      : 'border-muted-foreground/40',
                  ]"
                >
                  <div
                    v-if="formModel.mode === 'dhcp'"
                    class="size-1 rounded-full bg-white"
                  ></div>
                </div>
              </div>
              <p class="text-muted-foreground mt-2 text-[11px] leading-relaxed">
                {{ $t('ops.network.modeDHCPDesc') }}
              </p>
            </div>

            <!-- Static 模式卡片 -->
            <div
              class="cursor-pointer rounded-xl border-2 p-3.5 transition-all duration-150"
              :class="[
                formModel.mode === 'static'
                  ? 'border-primary bg-primary/5 dark:bg-primary/10 shadow-xs'
                  : 'border-border hover:border-primary/50 hover:bg-muted/30',
              ]"
              @click="selectIPMode('static')"
            >
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-2">
                  <div
                    class="flex size-6 items-center justify-center rounded-md"
                    :class="[
                      formModel.mode === 'static'
                        ? 'bg-primary text-primary-foreground'
                        : 'bg-muted text-muted-foreground',
                    ]"
                  >
                    <IconifyIcon icon="lucide:sliders" class="size-3.5" />
                  </div>
                  <span class="font-semibold text-xs">{{
                    $t('ops.network.modeStatic')
                  }}</span>
                </div>
                <div
                  class="size-3.5 rounded-full border flex items-center justify-center"
                  :class="[
                    formModel.mode === 'static'
                      ? 'border-primary bg-primary'
                      : 'border-muted-foreground/40',
                  ]"
                >
                  <div
                    v-if="formModel.mode === 'static'"
                    class="size-1 rounded-full bg-white"
                  ></div>
                </div>
              </div>
              <p class="text-muted-foreground mt-2 text-[11px] leading-relaxed">
                {{ $t('ops.network.modeStaticDesc') }}
              </p>
            </div>
          </div>
        </div>

        <!-- 主出口 (WAN) 开关卡片 -->
        <div>
          <label class="text-sm font-semibold text-foreground block mb-2">
            {{ $t('ops.network.isPrimary') }}
          </label>
          <div
            class="flex items-center justify-between rounded-xl border p-3.5 bg-card"
          >
            <div class="space-y-0.5 pr-2">
              <div class="text-xs font-semibold text-foreground">
                {{ $t('ops.network.setAsPrimary') }}
              </div>
              <div class="text-[11px] text-muted-foreground leading-relaxed">
                {{ $t('ops.network.primarySwitchTip') }}
              </div>
            </div>
            <Switch v-model:checked="formModel.primary" />
          </div>
        </div>

        <!-- 静态模式专属字段 (Static Form Fields) -->
        <template v-if="isStatic">
          <Divider class="!my-3" />

          <!-- IP 地址 -->
          <Form.Item :label="$t('ops.network.ipAddress')" required>
            <Input
              v-model:value="formModel.address"
              placeholder="例如 192.168.1.100"
              class="font-mono text-sm h-9"
            />
          </Form.Item>

          <!-- CIDR 前缀与掩码实时换算 -->
          <Form.Item :label="$t('ops.network.prefix')" required>
            <div class="flex items-center gap-3">
              <InputNumber
                v-model:value="formModel.prefix"
                :min="1"
                :max="32"
                class="w-32 font-mono h-9"
                placeholder="24"
              />
              <div
                class="text-xs text-muted-foreground flex items-center gap-1"
              >
                <span>{{ $t('ops.network.calculatedMask') }}:</span>
                <span class="font-mono font-semibold text-foreground">{{
                  calculatedSubnetMask
                }}</span>
              </div>
            </div>
          </Form.Item>

          <!-- 主出口专属网关与 DNS -->
          <template v-if="isPrimary">
            <Divider class="!my-3" />

            <Form.Item :label="$t('ops.network.gateway')" required>
              <Input
                v-model:value="formModel.gateway"
                placeholder="例如 192.168.1.1"
                class="font-mono text-sm h-9"
              />
            </Form.Item>

            <!-- DNS 服务器列表 -->
            <Form.Item :label="$t('ops.network.dnsServers')">
              <div class="space-y-2.5">
                <div
                  v-for="(_, index) in formModel.dnsServers"
                  :key="index"
                  class="flex items-center gap-2"
                >
                  <Input
                    v-model:value="formModel.dnsServers[index]"
                    placeholder="8.8.8.8"
                    class="font-mono text-sm h-9"
                  />
                  <Tooltip :title="$t('ops.network.removeDNS')">
                    <Button
                      danger
                      type="text"
                      class="shrink-0 flex items-center justify-center size-9 !p-0"
                      @click="removeDNSServer(index)"
                    >
                      <X class="size-4" />
                    </Button>
                  </Tooltip>
                </div>

                <Button
                  v-if="formModel.dnsServers.length < 3"
                  type="dashed"
                  block
                  size="small"
                  @click="addDNSServer"
                >
                  <Plus class="size-3.5 inline mr-1" />
                  {{ $t('ops.network.addDNS') }}
                </Button>

                <!-- 常用 DNS 推荐快捷填充 -->
                <div class="bg-muted/40 rounded-xl p-3 border mt-2">
                  <div
                    class="text-[11px] font-semibold text-muted-foreground mb-2 flex items-center gap-1.5"
                  >
                    <IconifyIcon
                      icon="lucide:sparkles"
                      class="size-3.5 text-amber-500"
                    />
                    <span>{{ $t('ops.network.quickDNS') }}</span>
                  </div>
                  <div class="flex flex-wrap gap-1.5">
                    <Tag
                      v-for="dns in recommendedDNS"
                      :key="dns.ip"
                      class="cursor-pointer font-mono text-[11px] hover:border-primary transition-colors py-0.5 px-2"
                      @click="quickFillDNS(dns.ip)"
                    >
                      + {{ dns.name }} ({{ dns.ip }})
                    </Tag>
                  </div>
                </div>
              </div>
            </Form.Item>
          </template>
        </template>
      </Form>

      <template #footer>
        <div class="flex justify-end gap-2.5">
          <Button @click="drawerVisible = false">{{
            $t('ops.network.cancel')
          }}</Button>
          <Button
            v-access:code="['ops:network:edit']"
            type="primary"
            :loading="submitting"
            @click="handleApply"
          >
            {{ $t('ops.network.confirm') }}
          </Button>
        </div>
      </template>
    </Drawer>

    <!-- 网络工作模式切换抽屉 (Mode Switch Drawer) -->
    <Drawer
      v-model:open="modeDrawerVisible"
      :title="targetMode === NETWORK_MODES.ActiveBackup ? $t('ops.network.modeEnter') : $t('ops.network.modeEnterGateway')"
      width="460px"
      :destroy-on-close="true"
    >
      <div class="space-y-5">
        <Alert
          v-if="targetMode === NETWORK_MODES.ActiveBackup"
          type="warning"
          show-icon
          :message="$t('ops.network.modeSwitchWarning')"
          class="!text-xs"
        />
        <Alert
          v-else
          type="warning"
          show-icon
          :message="$t('ops.network.modeGatewayWarning')"
          class="!text-xs"
        />

        <!-- Active-Backup 表单 -->
        <template v-if="targetMode === NETWORK_MODES.ActiveBackup">
          <!-- slave 选择 -->
          <div>
            <label class="text-sm font-semibold text-foreground block mb-2.5">
              {{ $t('ops.network.modeSlaveSelect') }}
            </label>
            <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
              <div
                v-for="iface in eligibleSlaves"
                :key="iface.id"
                class="cursor-pointer rounded-xl border-2 p-3 transition-all duration-150"
                :class="[
                  modeForm.slaveIds.includes(iface.id)
                    ? 'border-primary bg-primary/5 dark:bg-primary/10'
                    : 'border-border hover:border-primary/50 hover:bg-muted/30',
                ]"
                @click="toggleSlave(iface.id)"
              >
                <div class="flex items-center justify-between">
                  <div class="flex items-center gap-2">
                    <IconifyIcon
                      icon="lucide:ethernet-port"
                      class="size-4 text-muted-foreground"
                    />
                    <span class="font-semibold text-xs">{{
                      iface.displayName || iface.name
                    }}</span>
                  </div>
                  <div
                    class="size-3.5 rounded-full border flex items-center justify-center"
                    :class="[
                      modeForm.slaveIds.includes(iface.id)
                        ? 'border-primary bg-primary'
                        : 'border-muted-foreground/40',
                    ]"
                  >
                    <div
                      v-if="modeForm.slaveIds.includes(iface.id)"
                      class="size-1 rounded-full bg-white"
                    ></div>
                  </div>
                </div>
                <p class="text-muted-foreground mt-1.5 text-[11px] font-mono">
                  {{ iface.mac || '-' }}
                </p>
              </div>
            </div>
            <p class="text-muted-foreground mt-2 text-[11px]">
              {{ $t('ops.network.modeSlaveTip') }}
            </p>
          </div>

          <!-- primary 选择 -->
          <div>
            <label class="text-sm font-semibold text-foreground block mb-2.5">
              {{ $t('ops.network.modePrimarySelect') }}
            </label>
            <div class="flex flex-wrap gap-2">
              <Tag
                v-for="sid in modeForm.slaveIds"
                :key="sid"
                :color="modeForm.primarySlaveId === sid ? 'success' : 'default'"
                class="cursor-pointer font-mono border-0"
                @click="modeForm.primarySlaveId = sid"
              >
                {{ sid }}
              </Tag>
            </div>
          </div>

          <!-- bond 的 IPv4 配置 -->
          <div>
            <label class="text-sm font-semibold text-foreground block mb-2.5">
              {{ $t('ops.network.modeBondIPv4') }}
            </label>
            <div class="flex items-center gap-2 mb-3">
              <Tag
                :color="modeForm.ipv4.mode === 'dhcp' ? 'processing' : 'default'"
                class="cursor-pointer border-0"
                @click="modeForm.ipv4.mode = 'dhcp'"
              >
                {{ $t('ops.network.modeDHCP') }}
              </Tag>
              <Tag
                :color="modeForm.ipv4.mode === 'static' ? 'processing' : 'default'"
                class="cursor-pointer border-0"
                @click="modeForm.ipv4.mode = 'static'"
              >
                {{ $t('ops.network.modeStatic') }}
              </Tag>
              <Switch
                v-model:checked="modeForm.ipv4.primary"
                size="small"
                class="ml-auto"
              />
              <span class="text-xs text-muted-foreground">{{
                $t('ops.network.isPrimary')
              }}</span>
            </div>
            <template v-if="modeForm.ipv4.mode === 'static'">
              <Form layout="vertical" class="space-y-3">
                <Form.Item :label="$t('ops.network.ipAddress')">
                  <Input
                    v-model:value="modeForm.ipv4.address"
                    placeholder="192.168.1.100"
                    class="font-mono"
                  />
                </Form.Item>
                <Form.Item :label="$t('ops.network.prefix')">
                  <InputNumber
                    v-model:value="modeForm.ipv4.prefix"
                    :min="1"
                    :max="32"
                    class="w-full font-mono"
                  />
                </Form.Item>
                <template v-if="modeForm.ipv4.primary">
                  <Form.Item :label="$t('ops.network.gateway')">
                    <Input
                      v-model:value="modeForm.ipv4.gateway"
                      placeholder="192.168.1.1"
                      class="font-mono"
                    />
                  </Form.Item>
                  <Form.Item :label="$t('ops.network.dnsServers')">
                    <Input
                      :value="modeForm.ipv4.dnsServers?.[0] ?? ''"
                      placeholder="8.8.8.8"
                      class="font-mono"
                      @update:value="(val: string) => {
                        if (!modeForm.ipv4.dnsServers) {
                          modeForm.ipv4.dnsServers = [];
                        }
                        modeForm.ipv4.dnsServers[0] = val;
                      }"
                    />
                  </Form.Item>
                </template>
              </Form>
            </template>
          </div>
        </template>

        <!-- Gateway 表单 -->
        <template v-else-if="targetMode === NETWORK_MODES.Gateway">
          <!-- 下行 LAN 接口选择 -->
          <div>
            <label class="text-sm font-semibold text-foreground block mb-2.5">
              {{ $t('ops.network.modeGatewayDownstream') }}
            </label>
            <div class="grid grid-cols-1 gap-2">
              <div
                v-for="iface in eligibleGatewayIfaces"
                :key="iface.id"
                class="cursor-pointer rounded-xl border-2 p-3 transition-all duration-150"
                :class="[
                  modeForm.gateway.downstreamInterfaceId === iface.id
                    ? 'border-primary bg-primary/5 dark:bg-primary/10'
                    : 'border-border hover:border-primary/50 hover:bg-muted/30',
                ]"
                @click="() => {
                  modeForm.gateway.downstreamInterfaceId = iface.id;
                  onGatewayIfaceChange(iface.id);
                }"
              >
                <div class="flex items-center justify-between">
                  <div class="flex items-center gap-2">
                    <IconifyIcon
                      icon="lucide:router"
                      class="size-4 text-primary"
                    />
                    <span class="font-semibold text-xs">{{
                      iface.displayName || iface.name
                    }}</span>
                    <span class="font-mono text-xs text-muted-foreground">
                      ({{ iface.ipv4.address }}/{{ iface.ipv4.prefix }})
                    </span>
                  </div>
                  <div
                    class="size-3.5 rounded-full border flex items-center justify-center"
                    :class="[
                      modeForm.gateway.downstreamInterfaceId === iface.id
                        ? 'border-primary bg-primary'
                        : 'border-muted-foreground/40',
                    ]"
                  >
                    <div
                      v-if="modeForm.gateway.downstreamInterfaceId === iface.id"
                      class="size-1 rounded-full bg-white"
                    ></div>
                  </div>
                </div>
              </div>
            </div>
            <p class="text-muted-foreground mt-2 text-[11px]">
              {{ $t('ops.network.modeGatewayDownstreamTip') }}
            </p>
          </div>

          <!-- 地址池与租约配置 -->
          <Form layout="vertical" class="space-y-3">
            <Form.Item :label="$t('ops.network.modeGatewayPoolStart')" required>
              <Input
                v-model:value="modeForm.gateway.poolStart"
                placeholder="192.168.2.100"
                class="font-mono"
              />
            </Form.Item>
            <Form.Item :label="$t('ops.network.modeGatewayPoolEnd')" required>
              <Input
                v-model:value="modeForm.gateway.poolEnd"
                placeholder="192.168.2.200"
                class="font-mono"
              />
            </Form.Item>
            <Form.Item :label="$t('ops.network.modeGatewayPrefix')" required>
              <InputNumber
                v-model:value="modeForm.gateway.prefix"
                :min="1"
                :max="30"
                class="w-full font-mono"
              />
            </Form.Item>
            <Form.Item :label="$t('ops.network.modeGatewayLeaseDuration')">
              <InputNumber
                v-model:value="modeForm.gateway.leaseDurationSeconds"
                :min="60"
                :max="604800"
                class="w-full font-mono"
              />
            </Form.Item>

            <!-- ip_forward 三层转发开关及说明 -->
            <div class="rounded-xl border bg-muted/30 p-3.5 space-y-2">
              <div class="flex items-center justify-between">
                <span class="text-xs font-semibold text-foreground">
                  {{ $t('ops.network.modeGatewayIPForward') }}
                </span>
                <Switch
                  v-model:checked="modeForm.gateway.ipForward"
                  size="small"
                />
              </div>
              <p class="text-[11px] text-muted-foreground leading-relaxed">
                {{ $t('ops.network.modeGatewayIPForwardDesc') }}
              </p>
            </div>
          </Form>
        </template>

        <div class="flex justify-end gap-2.5">
          <Button @click="modeDrawerVisible = false">{{
            $t('ops.network.cancel')
          }}</Button>
          <Popconfirm
            :title="targetMode === NETWORK_MODES.ActiveBackup ? $t('ops.network.modeEnterConfirm') : $t('ops.network.modeSwitchWarning')"
            :disabled="targetMode === NETWORK_MODES.ActiveBackup ? modeForm.slaveIds.length !== 2 : !modeForm.gateway.downstreamInterfaceId"
            @confirm="handleModeSwitch"
          >
            <Button
              v-access:code="['ops:network:mode']"
              type="primary"
              :loading="modeSubmitting"
              :disabled="targetMode === NETWORK_MODES.ActiveBackup ? modeForm.slaveIds.length !== 2 : !modeForm.gateway.downstreamInterfaceId"
            >
              {{ $t('ops.network.confirm') }}
            </Button>
          </Popconfirm>
        </div>
      </div>
    </Drawer>
  </Page>
</template>
