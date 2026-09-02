<script lang="ts" setup>
import type { StorageApi } from '#/api';

import { computed, onMounted, onUnmounted, reactive, ref } from 'vue';

import { useAccess } from '@vben/access';
import { Page } from '@vben/common-ui';
import { CircleAlert, IconifyIcon, RotateCw } from '@vben/icons';
import { $t } from '@vben/locales';

import {
  Alert,
  Button,
  Divider,
  Form,
  FormItem,
  InputNumber,
  message,
  Modal,
  Progress,
  Slider,
  Spin,
  Tag,
  Tooltip,
} from 'ant-design-vue';
import dayjs from 'dayjs';

import {
  getStorageConfigApi,
  getStorageStatusApi,
  triggerStorageCleanupApi,
  updateStorageConfigApi,
} from '#/api';

const { hasAccessByCodes } = useAccess();
const hasEditPermission = computed(() =>
  hasAccessByCodes(['ops:storage:edit', 'super']),
);

const loading = ref(false);
const statusLoading = ref(false);
const saving = ref(false);
const cleaning = ref(false);

const storageStatus = ref<null | StorageApi.StorageStatus>(null);

const formState = reactive<StorageApi.UpdateStorageConfigInput>({
  autoCleanupEnabled: true,
  checkIntervalSeconds: 600,
  highWatermarkPercent: 85,
  lowWatermarkPercent: 70,
  retentionDays: 30,
});

let refreshTimer: null | ReturnType<typeof setInterval> = null;

const recommendedRetentionDays = [7, 15, 30, 90, 180];
const recommendedIntervals = [
  { label: '5 min', value: 300 },
  { label: '10 min', value: 600 },
  { label: '30 min', value: 1800 },
  { label: '1 h', value: 3600 },
];

function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const val = bytes / 1024 ** i;
  return `${val.toFixed(val >= 100 ? 1 : 2)} ${units[i]}`;
}

const isDegraded = computed(
  () =>
    storageStatus.value?.status === 'degraded' ||
    storageStatus.value?.circuitBreakerActive ||
    (storageStatus.value?.usagePercent ?? 0) >= 95,
);

const isCleaning = computed(
  () => storageStatus.value?.status === 'cleaning' || cleaning.value,
);

const usagePercent = computed(() => storageStatus.value?.usagePercent ?? 0);

const usageStatusColor = computed(() => {
  const percent = usagePercent.value;
  if (percent >= 95) return '#f43f5e';
  if (percent >= 85) return '#f59e0b';
  if (percent >= 70) return '#0284c7';
  return '#10b981';
});

const lastCleanupTimeStr = computed(() => {
  if (!storageStatus.value?.lastCleanupAt) return '-';
  return dayjs(storageStatus.value.lastCleanupAt).format('YYYY-MM-DD HH:mm:ss');
});

function quickSetRetentionDays(days: number) {
  if (!hasEditPermission.value) return;
  formState.retentionDays = days;
}

function quickSetInterval(seconds: number) {
  if (!hasEditPermission.value) return;
  formState.checkIntervalSeconds = seconds;
}

function selectAutoCleanup(enabled: boolean) {
  if (!hasEditPermission.value) return;
  formState.autoCleanupEnabled = enabled;
}

async function fetchStatus() {
  statusLoading.value = true;
  try {
    const res = await getStorageStatusApi();
    storageStatus.value = res;
  } catch (error) {
    console.error('Failed to fetch storage status:', error);
  } finally {
    statusLoading.value = false;
  }
}

async function fetchConfig() {
  loading.value = true;
  try {
    const res = await getStorageConfigApi();
    formState.autoCleanupEnabled = res.autoCleanupEnabled;
    formState.retentionDays = res.retentionDays;
    formState.highWatermarkPercent = res.highWatermarkPercent;
    formState.lowWatermarkPercent = res.lowWatermarkPercent;
    formState.checkIntervalSeconds = res.checkIntervalSeconds;
  } catch (error) {
    console.error('Failed to fetch storage config:', error);
  } finally {
    loading.value = false;
  }
}

async function handleSaveConfig() {
  if (formState.lowWatermarkPercent >= formState.highWatermarkPercent) {
    message.error(
      `${$t('ops.storage.lowWatermarkPercent')}必须小于${$t('ops.storage.highWatermarkPercent')}`,
    );
    return;
  }

  saving.value = true;
  try {
    await updateStorageConfigApi({ ...formState });
    message.success($t('ops.storage.saveSuccess'));
    await fetchConfig();
  } catch (error) {
    console.error('Failed to save storage config:', error);
  } finally {
    saving.value = false;
  }
}

function handleTriggerCleanup() {
  Modal.confirm({
    cancelText: $t('common.cancel'),
    content: $t('ops.storage.triggerCleanupConfirmContent'),
    icon: null,
    okButtonProps: { danger: true },
    okText: $t('common.confirm'),
    onOk: async () => {
      cleaning.value = true;
      try {
        await triggerStorageCleanupApi();
        message.success($t('ops.storage.triggerCleanupSuccess'));
        await fetchStatus();
      } catch (error) {
        console.error('Failed to trigger storage cleanup:', error);
      } finally {
        cleaning.value = false;
      }
    },
    title: $t('ops.storage.triggerCleanupConfirmTitle'),
  });
}

onMounted(async () => {
  await Promise.all([fetchConfig(), fetchStatus()]);
  refreshTimer = setInterval(() => {
    fetchStatus();
  }, 10_000);
});

onUnmounted(() => {
  if (refreshTimer) {
    clearInterval(refreshTimer);
    refreshTimer = null;
  }
});
</script>

<template>
  <Page auto-content-height>
    <div class="space-y-4 pb-6">
      <!-- 极危抓拍存盘熔断 Banner -->
      <Alert
        v-if="isDegraded"
        banner
        class="rounded-xl border border-rose-500/30 bg-rose-500/10 text-rose-700 dark:text-rose-300"
        type="error"
      >
        <template #icon>
          <CircleAlert class="size-5 animate-pulse text-rose-500" />
        </template>
        <template #message>
          <div
            class="flex flex-wrap items-center justify-between gap-2 font-semibold"
          >
            <span>{{ $t('ops.storage.circuitBreakerAlert') }}</span>
            <Tag color="error" class="border-0 font-medium">
              {{ $t('ops.storage.circuitBreakerActive') }}
            </Tag>
          </div>
        </template>
      </Alert>

      <!-- 顶部 4 列状态看板 -->
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <!-- 卡片 1：运行状态 & 极危熔断 -->
        <div
          class="bg-card text-card-foreground rounded-xl border p-5 shadow-xs transition-all duration-200"
        >
          <div class="flex items-center justify-between">
            <span class="text-muted-foreground text-sm font-medium">
              {{ $t('ops.storage.runningStatus') }}
            </span>
            <div
              class="flex size-8 items-center justify-center rounded-lg"
              :class="[
                isDegraded
                  ? 'bg-rose-500/10 text-rose-500 dark:bg-rose-500/20'
                  : isCleaning
                    ? 'bg-sky-500/10 text-sky-500 dark:bg-sky-500/20'
                    : 'bg-emerald-500/10 text-emerald-500 dark:bg-emerald-500/20',
              ]"
            >
              <IconifyIcon
                v-if="isDegraded"
                icon="lucide:alert-triangle"
                class="size-4.5 animate-pulse"
              />
              <IconifyIcon
                v-else-if="isCleaning"
                icon="lucide:refresh-cw"
                class="size-4.5 animate-spin"
              />
              <IconifyIcon v-else icon="lucide:shield-check" class="size-4.5" />
            </div>
          </div>
          <div class="mt-3 flex items-baseline gap-2">
            <div class="text-xl font-bold tracking-tight">
              {{
                isDegraded
                  ? $t('ops.storage.statusDegraded')
                  : isCleaning
                    ? $t('ops.storage.statusCleaning')
                    : $t('ops.storage.statusNormal')
              }}
            </div>
            <Tag
              :color="
                isDegraded ? 'error' : isCleaning ? 'processing' : 'success'
              "
              class="border-0 font-medium"
            >
              {{
                isDegraded
                  ? $t('ops.storage.circuitBreaker')
                  : isCleaning
                    ? 'FIFO'
                    : 'OK'
              }}
            </Tag>
          </div>
          <p class="text-muted-foreground mt-1 text-xs">
            {{
              isDegraded
                ? $t('ops.storage.circuitBreakerActive')
                : $t('ops.storage.level3Protected')
            }}
          </p>
        </div>

        <!-- 卡片 2：磁盘物理使用率 -->
        <div
          class="bg-card text-card-foreground rounded-xl border p-5 shadow-xs transition-all duration-200"
        >
          <div class="flex items-center justify-between">
            <span class="text-muted-foreground text-sm font-medium">
              {{ $t('ops.storage.usagePercent') }}
            </span>
            <div
              class="flex size-8 items-center justify-center rounded-lg"
              :style="{
                backgroundColor: `${usageStatusColor}15`,
                color: usageStatusColor,
              }"
            >
              <IconifyIcon icon="lucide:pie-chart" class="size-4.5" />
            </div>
          </div>
          <div
            class="mt-3 font-mono text-xl font-bold tracking-tight"
            :style="{ color: usageStatusColor }"
          >
            {{ usagePercent.toFixed(1) }}%
          </div>
          <p class="text-muted-foreground mt-1 text-xs">
            {{ $t('ops.storage.lowWatermarkPercent') }}
            {{ formState.lowWatermarkPercent }}% /
            {{ $t('ops.storage.highWatermarkPercent') }}
            {{ formState.highWatermarkPercent }}%
          </p>
        </div>

        <!-- 卡片 3：磁盘总容量与已用空间 -->
        <div
          class="bg-card text-card-foreground rounded-xl border p-5 shadow-xs transition-all duration-200"
        >
          <div class="flex items-center justify-between">
            <span class="text-muted-foreground text-sm font-medium">
              {{ $t('ops.storage.usedBytes') }} /
              {{ $t('ops.storage.totalBytes') }}
            </span>
            <div
              class="bg-primary/10 text-primary dark:bg-primary/20 flex size-8 items-center justify-center rounded-lg"
            >
              <IconifyIcon icon="lucide:hard-drive" class="size-4.5" />
            </div>
          </div>
          <div class="mt-3 flex items-baseline gap-1.5 truncate">
            <span
              class="font-mono text-xl font-bold tracking-tight text-foreground"
            >
              {{ formatBytes(storageStatus?.usedBytes ?? 0) }}
            </span>
            <span class="text-muted-foreground text-xs font-mono">
              / {{ formatBytes(storageStatus?.totalBytes ?? 0) }}
            </span>
          </div>
          <p class="text-muted-foreground mt-1 text-xs">
            {{ $t('ops.storage.freeBytes') }}:
            <span
              class="font-mono font-medium text-emerald-600 dark:text-emerald-400"
              >{{ formatBytes(storageStatus?.freeBytes ?? 0) }}</span>
          </p>
        </div>

        <!-- 卡片 4：上次清理与释放 -->
        <div
          class="bg-card text-card-foreground rounded-xl border p-5 shadow-xs transition-all duration-200"
        >
          <div class="flex items-center justify-between">
            <span class="text-muted-foreground text-sm font-medium">
              {{ $t('ops.storage.lastCleanupAt') }}
            </span>
            <div
              class="bg-sky-500/10 text-sky-500 dark:bg-sky-500/20 flex size-8 items-center justify-center rounded-lg"
            >
              <IconifyIcon icon="lucide:calendar-clock" class="size-4.5" />
            </div>
          </div>
          <div
            class="mt-3 truncate font-mono text-base font-bold tracking-tight"
          >
            {{ lastCleanupTimeStr }}
          </div>
          <div class="mt-1 flex items-center justify-between">
            <span class="text-muted-foreground text-xs">
              {{ $t('ops.storage.lastFreedBytes') }}:
              <span class="font-mono font-medium text-foreground">
                {{ formatBytes(storageStatus?.lastFreedBytes ?? 0) }}
              </span>
            </span>
            <Button
              type="link"
              size="small"
              class="!p-0 !h-auto text-xs"
              :loading="statusLoading"
              @click="fetchStatus"
            >
              <template #icon>
                <RotateCw class="mr-0.5 inline size-3" />
              </template>
              {{ $t('ops.storage.refreshStatus') }}
            </Button>
          </div>
        </div>
      </div>

      <!-- 主配置操作区域（两栏紧凑布局：左侧控制台 + 右侧技术规格与指南） -->
      <div class="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <!-- 左侧核心配置工作台（占2列） -->
        <div class="space-y-4 lg:col-span-2">
          <!-- 磁盘容量分析与业务数据量分布卡片 -->
          <div
            class="bg-card text-card-foreground rounded-xl border p-6 shadow-xs"
          >
            <div class="mb-5 flex items-center justify-between">
              <div>
                <h3 class="text-foreground text-base font-semibold">
                  {{ $t('ops.storage.storageDistribution') }}
                </h3>
                <p class="text-muted-foreground text-xs">
                  {{ $t('ops.storage.storageDistributionTip') }}
                </p>
              </div>
            </div>

            <!-- 大号多色阶水位刻度条 -->
            <div class="space-y-2">
              <div class="flex items-center justify-between text-xs">
                <span class="font-medium text-foreground">
                  {{ $t('ops.storage.usagePercent') }}:
                  <b class="font-mono" :style="{ color: usageStatusColor }">{{ usagePercent.toFixed(1) }}%</b>
                </span>
                <span class="text-muted-foreground font-mono">
                  {{ formatBytes(storageStatus?.usedBytes ?? 0) }} /
                  {{ formatBytes(storageStatus?.totalBytes ?? 0) }}
                </span>
              </div>
              <Progress
                :percent="usagePercent"
                :show-info="false"
                :stroke-color="usageStatusColor"
                :stroke-width="12"
                class="!m-0"
              />
              <div
                class="flex items-center justify-between pt-1 text-[11px] text-muted-foreground"
              >
                <span class="font-mono">0%</span>
                <span
                  class="flex items-center gap-1 font-mono text-sky-600 dark:text-sky-400"
                >
                  <span>{{ $t('ops.storage.lowWatermarkPercent') }}:</span>
                  <b>{{ formState.lowWatermarkPercent }}%</b>
                </span>
                <span
                  class="flex items-center gap-1 font-mono text-amber-600 dark:text-amber-400"
                >
                  <span>{{ $t('ops.storage.highWatermarkPercent') }}:</span>
                  <b>{{ formState.highWatermarkPercent }}%</b>
                </span>
                <span
                  class="flex items-center gap-1 font-mono text-rose-600 dark:text-rose-400"
                >
                  <span>{{ $t('ops.storage.circuitBreaker') }}:</span>
                  <b>95%</b>
                </span>
                <span class="font-mono">100%</span>
              </div>
            </div>

            <Divider class="my-5" />

            <!-- 业务数据量统计（五宫格现代化药丸块） -->
            <div>
              <div class="text-muted-foreground mb-3 text-xs font-semibold">
                {{ $t('ops.storage.statisticsTitle') }}
              </div>
              <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-5">
                <!-- 告警记录 -->
                <div
                  class="bg-muted/40 rounded-xl border p-3 transition-colors hover:bg-muted/70"
                >
                  <div class="flex items-center gap-2 text-rose-500">
                    <IconifyIcon icon="lucide:bell-ring" class="size-4" />
                    <span class="text-muted-foreground text-xs">{{
                      $t('ops.storage.alarmRecordCount')
                    }}</span>
                  </div>
                  <div class="mt-2 font-mono text-lg font-bold text-foreground">
                    {{ storageStatus?.alarmRecordCount ?? 0 }}
                  </div>
                </div>

                <!-- 车牌记录 -->
                <div
                  class="bg-muted/40 rounded-xl border p-3 transition-colors hover:bg-muted/70"
                >
                  <div class="flex items-center gap-2 text-sky-500">
                    <IconifyIcon icon="lucide:truck" class="size-4" />
                    <span class="text-muted-foreground text-xs">{{
                      $t('ops.storage.plateObservationCount')
                    }}</span>
                  </div>
                  <div class="mt-2 font-mono text-lg font-bold text-foreground">
                    {{ storageStatus?.plateObservationCount ?? 0 }}
                  </div>
                </div>

                <!-- 人脸识别 -->
                <div
                  class="bg-muted/40 rounded-xl border p-3 transition-colors hover:bg-muted/70"
                >
                  <div class="flex items-center gap-2 text-violet-500">
                    <IconifyIcon icon="lucide:user-check" class="size-4" />
                    <span class="text-muted-foreground text-xs">{{
                      $t('ops.storage.faceObservationCount')
                    }}</span>
                  </div>
                  <div class="mt-2 font-mono text-lg font-bold text-foreground">
                    {{ storageStatus?.faceObservationCount ?? 0 }}
                  </div>
                </div>

                <!-- 人脸抓拍 -->
                <div
                  class="bg-muted/40 rounded-xl border p-3 transition-colors hover:bg-muted/70"
                >
                  <div class="flex items-center gap-2 text-indigo-500">
                    <IconifyIcon icon="lucide:camera" class="size-4" />
                    <span class="text-muted-foreground text-xs">{{
                      $t('ops.storage.faceCaptureCount')
                    }}</span>
                  </div>
                  <div class="mt-2 font-mono text-lg font-bold text-foreground">
                    {{ storageStatus?.faceCaptureCount ?? 0 }}
                  </div>
                </div>

                <!-- 操作日志 -->
                <div
                  class="bg-muted/40 rounded-xl border p-3 transition-colors hover:bg-muted/70"
                >
                  <div class="flex items-center gap-2 text-zinc-500">
                    <IconifyIcon icon="lucide:file-text" class="size-4" />
                    <span class="text-muted-foreground text-xs">{{
                      $t('ops.storage.operationLogCount')
                    }}</span>
                  </div>
                  <div class="mt-2 font-mono text-lg font-bold text-foreground">
                    {{ storageStatus?.operationLogCount ?? 0 }}
                  </div>
                </div>
              </div>
            </div>
          </div>

          <!-- 存储自愈策略配置控制台卡片 -->
          <div
            class="bg-card text-card-foreground rounded-xl border p-6 shadow-xs"
          >
            <div class="mb-5 flex items-center justify-between">
              <div>
                <h3 class="text-foreground text-base font-semibold">
                  {{ $t('ops.storage.configTitle') }}
                </h3>
                <p class="text-muted-foreground text-xs">
                  {{ $t('ops.storage.configTip') }}
                </p>
              </div>
            </div>

            <Spin :spinning="loading">
              <div class="space-y-6">
                <!-- 自动清理启用模式双卡片选择器 -->
                <div>
                  <label
                    class="text-foreground mb-3 block text-sm font-semibold"
                  >
                    {{ $t('ops.storage.autoCleanupEnabled') }}
                  </label>
                  <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    <!-- 开启自动清理 -->
                    <div
                      class="cursor-pointer rounded-xl border-2 p-4 transition-all duration-150"
                      :class="[
                        formState.autoCleanupEnabled
                          ? 'border-primary bg-primary/5 dark:bg-primary/10 shadow-xs'
                          : 'border-border hover:border-primary/50 hover:bg-muted/30',
                        !hasEditPermission && 'cursor-not-allowed opacity-60',
                      ]"
                      @click="selectAutoCleanup(true)"
                    >
                      <div class="flex items-center justify-between">
                        <div class="flex items-center gap-2.5">
                          <div
                            class="flex size-7 items-center justify-center rounded-lg"
                            :class="[
                              formState.autoCleanupEnabled
                                ? 'bg-primary text-primary-foreground'
                                : 'bg-muted text-muted-foreground',
                            ]"
                          >
                            <IconifyIcon
                              icon="lucide:sparkles"
                              class="size-4"
                            />
                          </div>
                          <span class="text-sm font-semibold">{{
                            $t('ops.storage.autoCleanupEnabled')
                          }}</span>
                        </div>
                        <div
                          class="flex size-4 items-center justify-center rounded-full border"
                          :class="[
                            formState.autoCleanupEnabled
                              ? 'border-primary bg-primary'
                              : 'border-muted-foreground/40',
                          ]"
                        >
                          <div
                            v-if="formState.autoCleanupEnabled"
                            class="size-1.5 rounded-full bg-white"
                          ></div>
                        </div>
                      </div>
                      <p
                        class="text-muted-foreground mt-2 text-xs leading-relaxed"
                      >
                        {{ $t('ops.storage.autoCleanupEnabledDesc') }}
                      </p>
                    </div>

                    <!-- 停用自动清理 -->
                    <div
                      class="cursor-pointer rounded-xl border-2 p-4 transition-all duration-150"
                      :class="[
                        !formState.autoCleanupEnabled
                          ? 'border-primary bg-primary/5 dark:bg-primary/10 shadow-xs'
                          : 'border-border hover:border-primary/50 hover:bg-muted/30',
                        !hasEditPermission && 'cursor-not-allowed opacity-60',
                      ]"
                      @click="selectAutoCleanup(false)"
                    >
                      <div class="flex items-center justify-between">
                        <div class="flex items-center gap-2.5">
                          <div
                            class="flex size-7 items-center justify-center rounded-lg"
                            :class="[
                              !formState.autoCleanupEnabled
                                ? 'bg-primary text-primary-foreground'
                                : 'bg-muted text-muted-foreground',
                            ]"
                          >
                            <IconifyIcon
                              icon="lucide:pause-circle"
                              class="size-4"
                            />
                          </div>
                          <span class="text-sm font-semibold">{{
                            $t('ops.storage.autoCleanupDisabled')
                          }}</span>
                        </div>
                        <div
                          class="flex size-4 items-center justify-center rounded-full border"
                          :class="[
                            !formState.autoCleanupEnabled
                              ? 'border-primary bg-primary'
                              : 'border-muted-foreground/40',
                          ]"
                        >
                          <div
                            v-if="!formState.autoCleanupEnabled"
                            class="size-1.5 rounded-full bg-white"
                          ></div>
                        </div>
                      </div>
                      <p
                        class="text-muted-foreground mt-2 text-xs leading-relaxed"
                      >
                        {{ $t('ops.storage.autoCleanupDisabledDesc') }}
                      </p>
                    </div>
                  </div>
                </div>

                <Form layout="vertical">
                  <!-- 常规保留天数与快捷预设 -->
                  <FormItem class="!mb-5">
                    <template #label>
                      <div class="flex items-center gap-1.5">
                        <span class="font-semibold">{{
                          $t('ops.storage.retentionDays')
                        }}</span>
                        <Tooltip :title="$t('ops.storage.retentionDaysTip')">
                          <IconifyIcon
                            icon="lucide:help-circle"
                            class="text-muted-foreground size-3.5"
                          />
                        </Tooltip>
                      </div>
                    </template>
                    <div class="space-y-3">
                      <div class="flex flex-wrap items-center gap-3">
                        <InputNumber
                          v-model:value="formState.retentionDays"
                          :addon-after="$t('ops.storage.retentionDaysUnit')"
                          :disabled="!hasEditPermission"
                          :max="365"
                          :min="1"
                          class="w-48 font-mono"
                        />
                        <div class="flex flex-wrap items-center gap-1.5">
                          <Tag
                            v-for="days in recommendedRetentionDays"
                            :key="days"
                            class="hover:border-primary cursor-pointer font-mono text-xs transition-colors"
                            :class="[
                              formState.retentionDays === days
                                ? 'border-primary bg-primary/10 text-primary'
                                : '',
                            ]"
                            @click="quickSetRetentionDays(days)"
                          >
                            {{ days }} {{ $t('ops.storage.retentionDaysUnit') }}
                          </Tag>
                        </div>
                      </div>
                      <p class="text-muted-foreground text-xs">
                        {{ $t('ops.storage.retentionDaysTip') }}
                      </p>
                    </div>
                  </FormItem>

                  <!-- 高低水位滑动设置（两栏并排） -->
                  <div class="grid grid-cols-1 gap-5 md:grid-cols-2">
                    <!-- 高水位阈值 -->
                    <div class="bg-muted/30 rounded-xl border p-4">
                      <div class="mb-3 flex items-center justify-between">
                        <span class="text-sm font-semibold text-foreground">
                          {{ $t('ops.storage.highWatermarkPercent') }}
                        </span>
                        <span
                          class="font-mono text-sm font-bold text-amber-600 dark:text-amber-400"
                        >
                          {{ formState.highWatermarkPercent }}%
                        </span>
                      </div>
                      <div class="space-y-2">
                        <Slider
                          v-model:value="formState.highWatermarkPercent"
                          :disabled="!hasEditPermission"
                          :max="95"
                          :min="50"
                        />
                        <p class="text-muted-foreground text-xs">
                          {{ $t('ops.storage.highWatermarkPercentTip') }}
                        </p>
                      </div>
                    </div>

                    <!-- 低水位阈值 -->
                    <div class="bg-muted/30 rounded-xl border p-4">
                      <div class="mb-3 flex items-center justify-between">
                        <span class="text-sm font-semibold text-foreground">
                          {{ $t('ops.storage.lowWatermarkPercent') }}
                        </span>
                        <span
                          class="font-mono text-sm font-bold text-sky-600 dark:text-sky-400"
                        >
                          {{ formState.lowWatermarkPercent }}%
                        </span>
                      </div>
                      <div class="space-y-2">
                        <Slider
                          v-model:value="formState.lowWatermarkPercent"
                          :disabled="!hasEditPermission"
                          :max="90"
                          :min="30"
                        />
                        <p class="text-muted-foreground text-xs">
                          {{ $t('ops.storage.lowWatermarkPercentTip') }}
                        </p>
                      </div>
                    </div>
                  </div>

                  <!-- 巡检周期设置 -->
                  <FormItem class="!mt-5 !mb-2">
                    <template #label>
                      <div class="flex items-center gap-1.5">
                        <span class="font-semibold">{{
                          $t('ops.storage.checkIntervalSeconds')
                        }}</span>
                        <Tooltip
                          :title="$t('ops.storage.checkIntervalSecondsTip')"
                        >
                          <IconifyIcon
                            icon="lucide:help-circle"
                            class="text-muted-foreground size-3.5"
                          />
                        </Tooltip>
                      </div>
                    </template>
                    <div class="space-y-3">
                      <div class="flex flex-wrap items-center gap-3">
                        <InputNumber
                          v-model:value="formState.checkIntervalSeconds"
                          :addon-after="
                            $t('ops.storage.checkIntervalSecondsUnit')
                          "
                          :disabled="!hasEditPermission"
                          :max="86400"
                          :min="30"
                          class="w-48 font-mono"
                        />
                        <div class="flex flex-wrap items-center gap-1.5">
                          <Tag
                            v-for="item in recommendedIntervals"
                            :key="item.value"
                            class="hover:border-primary cursor-pointer font-mono text-xs transition-colors"
                            :class="[
                              formState.checkIntervalSeconds === item.value
                                ? 'border-primary bg-primary/10 text-primary'
                                : '',
                            ]"
                            @click="quickSetInterval(item.value)"
                          >
                            {{ item.label }}
                          </Tag>
                        </div>
                      </div>
                      <p class="text-muted-foreground text-xs">
                        {{ $t('ops.storage.checkIntervalSecondsTip') }}
                      </p>
                    </div>
                  </FormItem>
                </Form>

                <!-- 底部操作按钮栏 -->
                <div
                  class="flex items-center justify-between border-t border-border/60 pt-4"
                >
                  <div class="flex items-center gap-3">
                    <Button
                      v-access:code="['ops:storage:edit']"
                      type="primary"
                      :loading="saving"
                      class="px-6"
                      @click="handleSaveConfig"
                    >
                      <template #icon>
                        <IconifyIcon
                          icon="lucide:save"
                          class="mr-1.5 inline size-4"
                        />
                      </template>
                      {{ $t('ops.storage.saveConfig') }}
                    </Button>
                  </div>

                  <div>
                    <Button
                      v-access:code="['ops:storage:edit']"
                      danger
                      :loading="cleaning"
                      class="px-5"
                      @click="handleTriggerCleanup"
                    >
                      <template #icon>
                        <IconifyIcon
                          icon="lucide:trash-2"
                          class="mr-1.5 inline size-4"
                        />
                      </template>
                      {{ $t('ops.storage.triggerCleanup') }}
                    </Button>
                  </div>
                </div>
              </div>
            </Spin>
          </div>
        </div>

        <!-- 右侧使用指南与技术规格（占1列） -->
        <div class="space-y-4">
          <!-- 存储自愈三级防御说明卡片 -->
          <div
            class="bg-card text-card-foreground rounded-xl border p-5 shadow-xs"
          >
            <div
              class="text-foreground mb-4 flex items-center gap-2 text-sm font-semibold"
            >
              <IconifyIcon
                icon="lucide:shield-alert"
                class="text-primary size-4.5"
              />
              <span>{{ $t('ops.storage.operationGuide') }}</span>
            </div>
            <div
              class="text-muted-foreground space-y-3 text-xs leading-relaxed"
            >
              <p class="bg-muted/50 rounded-lg border p-3">
                {{ $t('ops.storage.guide1') }}
              </p>
              <p class="bg-muted/50 rounded-lg border p-3">
                {{ $t('ops.storage.guide2') }}
              </p>
              <p class="bg-muted/50 rounded-lg border p-3">
                {{ $t('ops.storage.guide3') }}
              </p>
            </div>
          </div>

          <!-- 底层技术规格卡片 -->
          <div
            class="bg-card text-card-foreground rounded-xl border p-5 shadow-xs"
          >
            <div
              class="text-foreground mb-3 flex items-center gap-2 text-sm font-semibold"
            >
              <IconifyIcon icon="lucide:cpu" class="size-4.5 text-sky-500" />
              <span>{{ $t('ops.storage.specsTitle') }}</span>
            </div>
            <div class="text-muted-foreground space-y-2 text-xs">
              <div
                class="border-border/50 flex justify-between border-b py-1.5"
              >
                <span>{{ $t('ops.storage.specLevel1') }}</span>
                <span class="text-foreground font-mono font-medium">{{
                  $t('ops.storage.specLevel1Value')
                }}</span>
              </div>
              <div
                class="border-border/50 flex justify-between border-b py-1.5"
              >
                <span>{{ $t('ops.storage.specLevel2') }}</span>
                <span class="text-foreground font-mono font-medium">{{
                  $t('ops.storage.specLevel2Value')
                }}</span>
              </div>
              <div
                class="border-border/50 flex justify-between border-b py-1.5"
              >
                <span>{{ $t('ops.storage.specLevel3') }}</span>
                <span
                  class="text-rose-600 dark:text-rose-400 font-mono font-medium"
                  >{{ $t('ops.storage.specLevel3Value') }}</span>
              </div>
              <div
                class="border-border/50 flex justify-between border-b py-1.5"
              >
                <span>{{ $t('ops.storage.specResetThreshold') }}</span>
                <span
                  class="text-emerald-600 dark:text-emerald-400 font-mono font-medium"
                  >{{ $t('ops.storage.specResetThresholdValue') }}</span>
              </div>
              <div
                class="border-border/50 flex justify-between border-b py-1.5"
              >
                <span>{{ $t('ops.storage.specWhitelist') }}</span>
                <span
                  class="text-emerald-600 dark:text-emerald-400 font-medium"
                  >{{ $t('ops.storage.specWhitelistValue') }}</span>
              </div>
              <div class="flex justify-between py-1.5">
                <span>{{ $t('ops.storage.specFreelist') }}</span>
                <span class="text-foreground font-mono font-medium">{{
                  $t('ops.storage.specFreelistValue')
                }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </Page>
</template>
