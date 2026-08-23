<script lang="ts" setup>
import type { Dayjs } from 'dayjs';

import type { NTPApi } from '#/api';

import { computed, onMounted, onUnmounted, ref } from 'vue';

import { Page } from '@vben/common-ui';
import {
  Check,
  CircleAlert,
  IconifyIcon,
  Plus,
  RotateCw,
  X,
} from '@vben/icons';
import { $t } from '@vben/locales';

import {
  Alert,
  Button,
  DatePicker,
  Divider,
  Empty,
  Form,
  Input,
  message,
  Modal,
  Spin,
  Tag,
  Tooltip,
} from 'ant-design-vue';
import dayjs from 'dayjs';

import {
  getNTPConfigApi,
  getNTPStatusApi,
  setNTPTimeApi,
  syncNTPNowApi,
  updateNTPConfigApi,
} from '#/api';

const loading = ref(false);
const statusLoading = ref(false);
const saving = ref(false);
const syncing = ref(false);
const applyingTime = ref(false);

const mode = ref<'manual' | 'ntp'>('ntp');
const servers = ref<string[]>([]);
const syncStatus = ref<NTPApi.SyncStatus | null>(null);

// 实时当前时钟显示
const currentTimeStr = ref(dayjs().format('YYYY-MM-DD HH:mm:ss'));
let timer: null | ReturnType<typeof setInterval> = null;

const manualDateTime = ref<Dayjs | undefined>(dayjs());

const recommendedServers = [
  'pool.ntp.org',
  'ntp.aliyun.com',
  'time.cloudflare.com',
  'time.google.com',
  'time.apple.com',
];

const isModeNTP = computed(() => mode.value === 'ntp');

async function fetchStatus() {
  statusLoading.value = true;
  try {
    const res = await getNTPStatusApi();
    syncStatus.value = res;
  } catch (error) {
    console.error('Failed to fetch ntp status:', error);
  } finally {
    statusLoading.value = false;
  }
}

async function fetchConfig() {
  loading.value = true;
  try {
    const res = await getNTPConfigApi();
    mode.value = res.mode;
    servers.value =
      res.servers && res.servers.length > 0
        ? [...res.servers]
        : ['pool.ntp.org', 'ntp.aliyun.com'];
  } catch (error) {
    console.error('Failed to fetch ntp config:', error);
  } finally {
    loading.value = false;
  }
}

function selectMode(targetMode: 'manual' | 'ntp') {
  mode.value = targetMode;
}

function addServer() {
  servers.value.push('');
}

function removeServer(index: number) {
  servers.value.splice(index, 1);
}

function quickAddServer(serverName: string) {
  if (!servers.value.includes(serverName)) {
    servers.value.push(serverName);
  }
}

async function handleSaveConfig() {
  const cleanServers = servers.value.map((s) => s.trim()).filter(Boolean);
  if (mode.value === 'ntp' && cleanServers.length === 0) {
    message.error($t('ops.time.serverRequired'));
    return;
  }

  saving.value = true;
  try {
    await updateNTPConfigApi({
      mode: mode.value,
      servers: cleanServers,
    });
    message.success($t('ops.time.saveSuccess'));
    await Promise.all([fetchConfig(), fetchStatus()]);
  } catch (error) {
    console.error('Failed to save config:', error);
  } finally {
    saving.value = false;
  }
}

async function handleSyncNow() {
  syncing.value = true;
  try {
    await syncNTPNowApi();
    message.success($t('ops.time.syncSuccess'));
    await fetchStatus();
  } catch (error) {
    console.error('Failed to sync now:', error);
  } finally {
    syncing.value = false;
  }
}

function handleApplyManualTime() {
  if (!manualDateTime.value) {
    message.error($t('ops.time.selectTime'));
    return;
  }
  const targetTime = manualDateTime.value;

  Modal.confirm({
    title: $t('ops.time.manualTime'),
    content: $t('ops.time.confirmApplyManualTime'),
    okText: $t('system.common.confirm'),
    cancelText: $t('system.common.cancel'),
    okType: 'danger',
    onOk: async () => {
      applyingTime.value = true;
      try {
        await setNTPTimeApi({
          time: targetTime.toISOString(),
        });
        message.success($t('ops.time.applyTimeSuccess'));
        await Promise.all([fetchConfig(), fetchStatus()]);
      } catch (error) {
        console.error('Failed to apply time:', error);
      } finally {
        applyingTime.value = false;
      }
    },
  });
}

onMounted(async () => {
  await Promise.all([fetchConfig(), fetchStatus()]);
  timer = setInterval(() => {
    currentTimeStr.value = dayjs().format('YYYY-MM-DD HH:mm:ss');
  }, 1000);
});

onUnmounted(() => {
  if (timer) {
    clearInterval(timer);
    timer = null;
  }
});
</script>

<template>
  <Page auto-content-height>
    <div class="space-y-4 pb-6">
      <!-- 顶部状态看板 -->
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <!-- 运行模式 & 状态卡片 -->
        <div
          class="bg-card text-card-foreground rounded-xl border p-5 shadow-xs transition-all duration-200"
        >
          <div class="flex items-center justify-between">
            <span class="text-muted-foreground text-sm font-medium">
              {{ $t('ops.time.currentRunningMode') }}
            </span>
            <div
              class="flex size-8 items-center justify-center rounded-lg" :class="[
                syncStatus?.synced
                  ? 'bg-emerald-500/10 text-emerald-500 dark:bg-emerald-500/20'
                  : 'bg-rose-500/10 text-rose-500 dark:bg-rose-500/20',
              ]"
            >
              <Check v-if="syncStatus?.synced" class="size-4.5" />
              <CircleAlert v-else class="size-4.5" />
            </div>
          </div>
          <div class="mt-3 flex items-baseline gap-2">
            <div class="text-xl font-bold tracking-tight">
              {{
                isModeNTP ? $t('ops.time.modeNTP') : $t('ops.time.modeManual')
              }}
            </div>
            <Tag
              :color="syncStatus?.synced ? 'success' : 'default'"
              class="border-0 font-medium"
            >
              {{
                syncStatus?.synced
                  ? $t('ops.time.synced')
                  : $t('ops.time.notSynced')
              }}
            </Tag>
          </div>
          <p class="text-muted-foreground mt-1 text-xs">
            {{
              isModeNTP
                ? $t('ops.time.daemonActive')
                : $t('ops.time.manualActive')
            }}
          </p>
        </div>

        <!-- 实时系统时钟卡片 -->
        <div
          class="bg-card text-card-foreground rounded-xl border p-5 shadow-xs transition-all duration-200"
        >
          <div class="flex items-center justify-between">
            <span class="text-muted-foreground text-sm font-medium">
              {{ $t('ops.time.systemOverview') }}
            </span>
            <div
              class="bg-primary/10 text-primary dark:bg-primary/20 flex size-8 items-center justify-center rounded-lg"
            >
              <IconifyIcon icon="lucide:clock-4" class="size-4.5" />
            </div>
          </div>
          <div
            class="mt-3 font-mono text-xl font-bold tracking-tight text-primary"
          >
            {{ currentTimeStr }}
          </div>
          <p class="text-muted-foreground mt-1 text-xs">
            {{ $t('ops.time.localTimeTip') }}
          </p>
        </div>

        <!-- 同步源 / 时钟偏移卡片 -->
        <div
          class="bg-card text-card-foreground rounded-xl border p-5 shadow-xs transition-all duration-200"
        >
          <div class="flex items-center justify-between">
            <span class="text-muted-foreground text-sm font-medium">
              {{ isModeNTP ? $t('ops.time.source') : $t('ops.time.offset') }}
            </span>
            <div
              class="bg-amber-500/10 text-amber-500 dark:bg-amber-500/20 flex size-8 items-center justify-center rounded-lg"
            >
              <IconifyIcon icon="lucide:server" class="size-4.5" />
            </div>
          </div>
          <div class="mt-3 truncate text-xl font-bold tracking-tight">
            {{
              isModeNTP
                ? syncStatus?.source || $t('ops.time.emptyValue')
                : syncStatus?.offset || $t('ops.time.zeroOffset')
            }}
          </div>
          <p class="text-muted-foreground mt-1 text-xs">
            {{
              isModeNTP
                ? $t('ops.time.validSourceTip')
                : $t('ops.time.offsetTip')
            }}
          </p>
        </div>

        <!-- 最近核对时间卡片 -->
        <div
          class="bg-card text-card-foreground rounded-xl border p-5 shadow-xs transition-all duration-200"
        >
          <div class="flex items-center justify-between">
            <span class="text-muted-foreground text-sm font-medium">
              {{ $t('ops.time.lastSyncTime') }}
            </span>
            <div
              class="bg-sky-500/10 text-sky-500 dark:bg-sky-500/20 flex size-8 items-center justify-center rounded-lg"
            >
              <IconifyIcon icon="lucide:calendar-check" class="size-4.5" />
            </div>
          </div>
          <div class="mt-3 truncate text-base font-bold tracking-tight">
            {{
              syncStatus?.lastSyncTime
                ? dayjs(syncStatus.lastSyncTime).format('HH:mm:ss YYYY-MM-DD')
                : $t('ops.time.emptyValue')
            }}
          </div>
          <div class="mt-1 flex items-center justify-between">
            <span class="text-muted-foreground text-xs">{{
              $t('ops.time.lastCheckTip')
            }}</span>
            <Button
              type="link"
              size="small"
              class="!p-0 !h-auto text-xs"
              :loading="statusLoading"
              @click="fetchStatus"
            >
              <template #icon>
                <RotateCw class="size-3 mr-0.5 inline" />
              </template>
              {{ $t('ops.time.refreshStatus') }}
            </Button>
          </div>
        </div>
      </div>

      <!-- 主配置操作区域（两栏紧凑布局：左侧控制台 + 右侧模式与指南） -->
      <div class="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <!-- 左侧核心配置工作台（占2列） -->
        <div class="lg:col-span-2 space-y-4">
          <div
            class="bg-card text-card-foreground rounded-xl border p-6 shadow-xs"
          >
            <!-- 模式选择卡片区 -->
            <div class="mb-6">
              <label class="text-sm font-semibold text-foreground block mb-3">
                {{ $t('ops.time.switchModeTip') }}
              </label>
              <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <!-- NTP 模式卡片 -->
                <div
                  class="cursor-pointer rounded-xl border-2 p-4 transition-all duration-150" :class="[
                    isModeNTP
                      ? 'border-primary bg-primary/5 dark:bg-primary/10 shadow-xs'
                      : 'border-border hover:border-primary/50 hover:bg-muted/30',
                  ]"
                  @click="selectMode('ntp')"
                >
                  <div class="flex items-center justify-between">
                    <div class="flex items-center gap-2.5">
                      <div
                        class="flex size-7 items-center justify-center rounded-lg" :class="[
                          isModeNTP
                            ? 'bg-primary text-primary-foreground'
                            : 'bg-muted text-muted-foreground',
                        ]"
                      >
                        <IconifyIcon icon="lucide:globe" class="size-4" />
                      </div>
                      <span class="font-semibold text-sm">{{
                        $t('ops.time.modeNTP')
                      }}</span>
                    </div>
                    <div
                      class="size-4 rounded-full border flex items-center justify-center" :class="[
                        isModeNTP
                          ? 'border-primary bg-primary'
                          : 'border-muted-foreground/40',
                      ]"
                    >
                      <div
                        v-if="isModeNTP"
                        class="size-1.5 rounded-full bg-white"
                      ></div>
                    </div>
                  </div>
                  <p class="text-muted-foreground mt-2 text-xs leading-relaxed">
                    {{ $t('ops.time.modeNTPDesc') }}
                  </p>
                </div>

                <!-- 手动模式卡片 -->
                <div
                  class="cursor-pointer rounded-xl border-2 p-4 transition-all duration-150" :class="[
                    !isModeNTP
                      ? 'border-primary bg-primary/5 dark:bg-primary/10 shadow-xs'
                      : 'border-border hover:border-primary/50 hover:bg-muted/30',
                  ]"
                  @click="selectMode('manual')"
                >
                  <div class="flex items-center justify-between">
                    <div class="flex items-center gap-2.5">
                      <div
                        class="flex size-7 items-center justify-center rounded-lg" :class="[
                          !isModeNTP
                            ? 'bg-primary text-primary-foreground'
                            : 'bg-muted text-muted-foreground',
                        ]"
                      >
                        <IconifyIcon icon="lucide:sliders" class="size-4" />
                      </div>
                      <span class="font-semibold text-sm">{{
                        $t('ops.time.modeManual')
                      }}</span>
                    </div>
                    <div
                      class="size-4 rounded-full border flex items-center justify-center" :class="[
                        !isModeNTP
                          ? 'border-primary bg-primary'
                          : 'border-muted-foreground/40',
                      ]"
                    >
                      <div
                        v-if="!isModeNTP"
                        class="size-1.5 rounded-full bg-white"
                      ></div>
                    </div>
                  </div>
                  <p class="text-muted-foreground mt-2 text-xs leading-relaxed">
                    {{ $t('ops.time.modeManualDesc') }}
                  </p>
                </div>
              </div>
            </div>

            <Divider class="!my-5" />

            <Spin :spinning="loading">
              <!-- NTP 配置表单 -->
              <div v-if="isModeNTP" class="space-y-6">
                <div>
                  <div class="flex items-center justify-between mb-2">
                    <div>
                      <label class="text-sm font-semibold text-foreground">
                        {{ $t('ops.time.serverList') }}
                      </label>
                      <p class="text-muted-foreground text-xs mt-0.5">
                        {{ $t('ops.time.serverListDesc') }}
                      </p>
                    </div>
                    <Button
                      type="dashed"
                      size="small"
                      v-access:code="['ops:time:edit']"
                      @click="addServer"
                    >
                      <template #icon>
                        <Plus class="size-3.5 inline mr-1" />
                      </template>
                      {{ $t('ops.time.addServer') }}
                    </Button>
                  </div>

                  <div
                    v-if="servers.length === 0"
                    class="rounded-lg border border-dashed p-8 text-center"
                  >
                    <Empty :description="$t('ops.time.serverRequired')" />
                  </div>

                  <div v-else class="space-y-3 mt-3">
                    <div
                      v-for="(_, index) in servers"
                      :key="index"
                      class="flex items-center gap-2.5"
                    >
                      <div
                        class="bg-muted text-muted-foreground flex size-8 shrink-0 items-center justify-center rounded-lg font-mono text-xs font-semibold"
                      >
                        0{{ index + 1 }}
                      </div>
                      <Input
                        v-model:value="servers[index]"
                        :placeholder="$t('ops.time.serverPlaceholder')"
                        allow-clear
                        class="h-9 font-mono text-sm"
                      />
                      <Tooltip :title="$t('ops.time.removeServer')">
                        <Button
                          v-access:code="['ops:time:edit']"
                          danger
                          type="text"
                          :disabled="servers.length <= 1"
                          class="shrink-0 flex items-center justify-center size-9 !p-0"
                          @click="removeServer(index)"
                        >
                          <X class="size-4" />
                        </Button>
                      </Tooltip>
                    </div>
                  </div>
                </div>

                <!-- 推荐服务器快捷选择 -->
                <div class="bg-muted/40 rounded-xl p-3.5 border">
                  <div
                    class="text-xs font-semibold text-muted-foreground mb-2 flex items-center gap-1.5"
                  >
                    <IconifyIcon
                      icon="lucide:sparkles"
                      class="size-3.5 text-amber-500"
                    />
                    <span>{{ $t('ops.time.quickFill') }}</span>
                  </div>
                  <div class="flex flex-wrap gap-2">
                    <Tag
                      v-for="srv in recommendedServers"
                      :key="srv"
                      v-access:code="['ops:time:edit']"
                      class="cursor-pointer font-mono text-xs hover:border-primary transition-colors py-0.5 px-2"
                      @click="quickAddServer(srv)"
                    >
                      + {{ srv }}
                    </Tag>
                  </div>
                </div>

                <div class="flex items-center gap-3 pt-2">
                  <Button
                    v-access:code="['ops:time:edit']"
                    type="primary"
                    :loading="saving"
                    class="px-6"
                    @click="handleSaveConfig"
                  >
                    {{ $t('ops.time.saveConfig') }}
                  </Button>
                  <Button
                    v-access:code="['ops:time:edit']"
                    :loading="syncing"
                    class="px-5"
                    @click="handleSyncNow"
                  >
                    <template #icon>
                      <RotateCw class="size-4 inline mr-1.5" />
                    </template>
                    {{ $t('ops.time.syncNow') }}
                  </Button>
                </div>
              </div>

              <!-- 手动模式表单 -->
              <div v-else class="space-y-5">
                <Alert
                  type="warning"
                  show-icon
                  class="rounded-xl border"
                  :message="$t('ops.time.manualTime')"
                  :description="$t('ops.time.confirmApplyManualTime')"
                />

                <Form layout="vertical">
                  <Form.Item :label="$t('ops.time.selectTime')" required>
                    <DatePicker
                      v-model:value="manualDateTime"
                      show-time
                      format="YYYY-MM-DD HH:mm:ss"
                      class="w-full h-10 font-mono"
                    />
                  </Form.Item>

                  <div class="flex items-center gap-3 pt-2">
                    <Button
                      v-access:code="['ops:time:edit']"
                      type="primary"
                      danger
                      :loading="applyingTime"
                      class="px-6"
                      @click="handleApplyManualTime"
                    >
                      <template #icon>
                        <IconifyIcon icon="lucide:clock-3" />
                      </template>
                      {{ $t('ops.time.applyTime') }}
                    </Button>
                  </div>
                </Form>
              </div>
            </Spin>
          </div>
        </div>

        <!-- 右侧使用指南与说明（占1列，丰富页面信息密度） -->
        <div class="space-y-4">
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
              <span>{{ $t('ops.time.operationGuide') }}</span>
            </div>
            <div
              class="space-y-3.5 text-xs text-muted-foreground leading-relaxed"
            >
              <p class="bg-muted/50 p-3 rounded-lg border">
                {{ $t('ops.time.guide1') }}
              </p>
              <p class="bg-muted/50 p-3 rounded-lg border">
                {{ $t('ops.time.guide2') }}
              </p>
              <p class="bg-muted/50 p-3 rounded-lg border">
                {{ $t('ops.time.guide3') }}
              </p>
            </div>
          </div>

          <div
            class="bg-card text-card-foreground rounded-xl border p-5 shadow-xs"
          >
            <div
              class="flex items-center gap-2 text-foreground font-semibold text-sm mb-3"
            >
              <IconifyIcon icon="lucide:info" class="size-4.5 text-sky-500" />
              <span>{{ $t('ops.time.techSpecs') }}</span>
            </div>
            <div class="space-y-2 text-xs text-muted-foreground">
              <div class="flex justify-between py-1 border-b border-border/50">
                <span>{{ $t('ops.time.syncProtocol') }}</span>
                <span class="font-mono font-medium text-foreground">{{
                  $t('ops.time.syncProtocolValue')
                }}</span>
              </div>
              <div class="flex justify-between py-1 border-b border-border/50">
                <span>{{ $t('ops.time.daemon') }}</span>
                <span class="font-mono font-medium text-foreground">{{
                  $t('ops.time.daemonValue')
                }}</span>
              </div>
              <div class="flex justify-between py-1 border-b border-border/50">
                <span>{{ $t('ops.time.hwClock') }}</span>
                <span class="font-mono font-medium text-foreground">{{
                  $t('ops.time.hwClockValue')
                }}</span>
              </div>
              <div class="flex justify-between py-1">
                <span>{{ $t('ops.time.eventFlag') }}</span>
                <span class="font-mono font-medium text-foreground">{{
                  $t('ops.time.eventFlagValue')
                }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </Page>
</template>
