<script lang="ts" setup>
import type { AlgorithmApi } from '#/api';

import { computed, onMounted, onUnmounted, ref } from 'vue';

import { Page } from '@vben/common-ui';
import { IconifyIcon } from '@vben/icons';
import { $t } from '@vben/locales';
import { formatDateTime } from '@vben/utils';

import {
  Button,
  Input,
  Pagination,
  Radio,
  Select,
  Skeleton,
  Tag,
  Tooltip,
} from 'ant-design-vue';

import { getAlgorithmList } from '#/api';
import { copyToClipboard } from '#/utils/clipboard';
import {
  formatAlarmTypeName,
  formatAlgorithmDesc,
  formatAlgorithmName,
} from '#/utils/i18n';

import SchemaModal from './components/SchemaModal.vue';
import UploadModal from './components/UploadModal.vue';
import VersionsDrawer from './components/VersionsDrawer.vue';

// 模态框与抽屉状态
const uploadModalOpen = ref(false);
const versionsDrawerOpen = ref(false);
const schemaModalOpen = ref(false);

const currentSelectedAlgo = ref<AlgorithmApi.AlgorithmItem | null>(null);
const currentSchema = ref<Record<string, unknown>>({});
const currentSchemaTitle = ref('');

// 数据与分页状态
const loading = ref(false);
const rawList = ref<AlgorithmApi.AlgorithmItem[]>([]);
const total = ref(0);
const currentPage = ref(1);
const pageSize = ref(12);

// 搜索与过滤状态
const searchKeyword = ref('');
const selectedType = ref<string>('all');
const selectedSource = ref<'all' | 'builtin' | 'custom'>('all');

// 过滤后的卡片列表
const displayList = computed(() => rawList.value);

// 顶部统计指标
const statsSummary = computed(() => {
  const totalCount = total.value > 0 ? total.value : rawList.value.length;
  let activeCount = 0;
  let builtinCount = 0;
  let customCount = 0;
  const platformSet = new Set<string>();

  rawList.value.forEach((algo) => {
    if (algo.activeVersion) {
      activeCount++;
    }
    if (algo.isBuiltin) {
      builtinCount++;
    } else {
      customCount++;
    }
    if (Array.isArray(algo.versions)) {
      algo.versions.forEach((v) => {
        if (v.platformId) platformSet.add(v.platformId);
      });
    }
  });

  return {
    totalCount,
    activeCount,
    builtinCount,
    customCount,
    platformsCount: platformSet.size,
    platforms: [...platformSet],
  };
});

const primaryPlatformDisplay = computed(() => {
  const platforms = statsSummary.value.platforms;
  if (platforms.length === 0) return 'darwin-arm64';
  if (platforms.length === 1) return platforms[0];
  return `${platforms.length} ${$t('ai.algorithm.statsPlatforms')}`;
});

const platformSubtitleDisplay = computed(() => {
  const platforms = statsSummary.value.platforms;
  if (platforms.length <= 1) return $t('ai.algorithm.hardwareReady');
  return platforms.join(', ');
});

// 加载算法列表数据
async function loadData(page = currentPage.value, size = pageSize.value) {
  loading.value = true;
  try {
    const params: AlgorithmApi.AlgorithmPageQuery = {
      page,
      pageSize: size,
    };
    if (searchKeyword.value.trim()) {
      params.keyword = searchKeyword.value.trim();
    }
    if (selectedType.value !== 'all') {
      params.algorithmType = selectedType.value;
    }
    if (selectedSource.value === 'builtin') {
      params.isBuiltin = true;
    } else if (selectedSource.value === 'custom') {
      params.isBuiltin = false;
    }

    const res = await getAlgorithmList(params);
    rawList.value = res.items || [];
    total.value = res.total || 0;
    currentPage.value = page;
    pageSize.value = size;
  } finally {
    loading.value = false;
  }
}

// 搜索防抖 / 监听
let searchTimer: null | ReturnType<typeof setTimeout> = null;
function handleSearchInput() {
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    currentPage.value = 1;
    loadData(1, pageSize.value);
  }, 300);
}

function handleTypeChange() {
  currentPage.value = 1;
  loadData(1, pageSize.value);
}

function handleSourceChange() {
  currentPage.value = 1;
  loadData(1, pageSize.value);
}

function handleResetFilter() {
  searchKeyword.value = '';
  selectedType.value = 'all';
  selectedSource.value = 'all';
  currentPage.value = 1;
  loadData(1, pageSize.value);
}

// 复制算法标识
async function handleCopyId(id: string) {
  await copyToClipboard(id, `${$t('ai.algorithm.copied')}: ${id}`);
}

// 打开版本管理抽屉
function handleOpenVersions(algo: AlgorithmApi.AlgorithmItem) {
  currentSelectedAlgo.value = algo;
  versionsDrawerOpen.value = true;
}

// 打开参数规范弹窗
function handleOpenActiveSchema(algo: AlgorithmApi.AlgorithmItem) {
  const activeVer = algo.versions?.find(
    (v) => v.version === algo.activeVersion,
  );
  let schema = activeVer?.configSchema;
  if (typeof schema === 'string') {
    try {
      schema = JSON.parse(schema);
    } catch {
      schema = {};
    }
  }
  currentSchema.value = (schema || {}) as Record<string, unknown>;
  currentSchemaTitle.value = `[${algo.algorithmId} v${algo.activeVersion || '1.0.0'}] ${$t('ai.algorithm.schemaBtn')}`;
  schemaModalOpen.value = true;
}

// 从版本抽屉打开某版本的参数规范
function handleViewSchemaFromDrawer(
  schema: Record<string, unknown>,
  title: string,
) {
  currentSchema.value = schema;
  currentSchemaTitle.value = title;
  schemaModalOpen.value = true;
}

function handleUploadSuccess() {
  loadData(1, pageSize.value);
}

function handleDrawerRefresh() {
  loadData(currentPage.value, pageSize.value);
}

// 样式与图标辅助函数
function getCategoryInfo(algoType?: string) {
  switch (algoType) {
    case 'face_recognition': {
      return {
        icon: 'lucide:scan-face',
        color: 'text-violet-500',
        bg: 'bg-violet-500/10 dark:bg-violet-500/20',
        badgeBg: 'bg-violet-500/15 text-violet-600 dark:text-violet-400',
        borderHover: 'hover:border-violet-500/40',
        label: $t('ai.algorithm.typeFace'),
      };
    }
    case 'license_plate_recognition': {
      return {
        icon: 'lucide:car',
        color: 'text-amber-500',
        bg: 'bg-amber-500/10 dark:bg-amber-500/20',
        badgeBg: 'bg-amber-500/15 text-amber-600 dark:text-amber-400',
        borderHover: 'hover:border-amber-500/40',
        label: $t('ai.algorithm.typePlate'),
      };
    }
    case 'object_detection': {
      return {
        icon: 'lucide:scan',
        color: 'text-emerald-500',
        bg: 'bg-emerald-500/10 dark:bg-emerald-500/20',
        badgeBg: 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-400',
        borderHover: 'hover:border-emerald-500/40',
        label: $t('ai.algorithm.typeDetection'),
      };
    }
    default: {
      return {
        icon: 'lucide:cpu',
        color: 'text-sky-500',
        bg: 'bg-sky-500/10 dark:bg-sky-500/20',
        badgeBg: 'bg-sky-500/15 text-sky-600 dark:text-sky-400',
        borderHover: 'hover:border-sky-500/40',
        label: $t('ai.algorithm.typeOther'),
      };
    }
  }
}

// 获取算法当前激活版本或所有支持的平台集合
function getSupportedPlatforms(algo: AlgorithmApi.AlgorithmItem): string[] {
  const set = new Set<string>();
  if (Array.isArray(algo.versions)) {
    algo.versions.forEach((v) => {
      if (v.platformId) set.add(v.platformId);
    });
  }
  return [...set];
}

// 获取激活版本的 FPS 档位
function getActiveFPSTiers(algo: AlgorithmApi.AlgorithmItem) {
  const activeVer = algo.versions?.find(
    (v) => v.version === algo.activeVersion,
  );
  if (
    activeVer &&
    Array.isArray(activeVer.fpsTiers) &&
    activeVer.fpsTiers.length > 0
  ) {
    return activeVer.fpsTiers;
  }
  return [];
}

onMounted(() => {
  loadData();
});

onUnmounted(() => {
  if (searchTimer) {
    clearTimeout(searchTimer);
    searchTimer = null;
  }
});
</script>

<template>
  <Page auto-content-height>
    <div class="space-y-4 pb-6">
      <!-- 1. 顶部数据指标看板 -->
      <div class="grid grid-cols-2 gap-3 sm:grid-cols-4 sm:gap-4">
        <!-- 卡片 1：算法总数 -->
        <div
          class="rounded-xl border border-border bg-card p-4 shadow-xs transition-all duration-200"
        >
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium text-muted-foreground">
              {{ $t('ai.algorithm.statsTotal') }}
            </span>
            <div
              class="flex size-7 items-center justify-center rounded-lg bg-primary/10 text-primary"
            >
              <IconifyIcon icon="lucide:boxes" class="size-4" />
            </div>
          </div>
          <div class="mt-2 text-2xl font-bold tracking-tight text-foreground">
            {{ statsSummary.totalCount }}
          </div>
          <p class="mt-1 text-[11px] text-muted-foreground">
            {{ statsSummary.builtinCount }}
            {{ $t('ai.algorithm.statsBuiltin') }} /
            {{ statsSummary.customCount }} {{ $t('ai.algorithm.statsCustom') }}
          </p>
        </div>

        <!-- 卡片 2：运行中模型 -->
        <div
          class="rounded-xl border border-border bg-card p-4 shadow-xs transition-all duration-200"
        >
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium text-muted-foreground">
              {{ $t('ai.algorithm.statsActive') }}
            </span>
            <div
              class="flex size-7 items-center justify-center rounded-lg bg-emerald-500/10 text-emerald-600 dark:bg-emerald-500/20 dark:text-emerald-400"
            >
              <IconifyIcon icon="lucide:activity" class="size-4" />
            </div>
          </div>
          <div class="mt-2 flex items-baseline gap-1.5">
            <span
              class="text-2xl font-bold tracking-tight text-emerald-600 dark:text-emerald-400"
            >
              {{ statsSummary.activeCount }}
            </span>
            <span class="text-xs text-muted-foreground">
              / {{ statsSummary.totalCount }}
            </span>
          </div>
          <p
            class="mt-1 text-[11px] text-muted-foreground flex items-center gap-1"
          >
            <span
              class="size-1.5 rounded-full bg-emerald-500 animate-pulse"
            ></span>
            <span>{{ $t('ai.algorithm.activeModel') }}</span>
          </p>
        </div>

        <!-- 卡片 3：平台架构 -->
        <div
          class="rounded-xl border border-border bg-card p-4 shadow-xs transition-all duration-200"
        >
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium text-muted-foreground">
              {{ $t('ai.algorithm.statsPlatforms') }}
            </span>
            <div
              class="flex size-7 items-center justify-center rounded-lg bg-sky-500/10 text-sky-600 dark:bg-sky-500/20 dark:text-sky-400"
            >
              <IconifyIcon icon="lucide:cpu" class="size-4" />
            </div>
          </div>
          <div
            class="mt-2 truncate font-mono text-lg font-bold tracking-tight text-foreground"
            :title="primaryPlatformDisplay"
          >
            {{ primaryPlatformDisplay }}
          </div>
          <p class="mt-1 truncate text-[11px] text-muted-foreground">
            {{ platformSubtitleDisplay }}
          </p>
        </div>

        <!-- 卡片 4：快捷安装入口 -->
        <div
          class="rounded-xl border border-dashed border-primary/40 bg-primary/5 p-4 shadow-xs hover:bg-primary/10 transition-all duration-200 flex flex-col justify-between cursor-pointer group"
          @click="uploadModalOpen = true"
        >
          <div class="flex items-center justify-between">
            <span class="text-xs font-semibold text-primary">
              {{ $t('ai.algorithm.upload') }}
            </span>
            <div
              class="flex size-7 items-center justify-center rounded-lg bg-primary text-primary-foreground shadow-xs group-hover:scale-105 transition-transform"
            >
              <IconifyIcon icon="lucide:upload-cloud" class="size-4" />
            </div>
          </div>
          <div class="mt-2">
            <span class="text-xs font-medium text-foreground">
              {{ $t('ai.algorithm.uploadTitle') }}
            </span>
            <p class="text-[11px] text-muted-foreground truncate">
              YOLO / CoreML / RKNN
            </p>
          </div>
        </div>
      </div>

      <!-- 2. 搜索与过滤工具栏 -->
      <div
        class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border bg-card p-3 shadow-xs"
      >
        <div class="flex flex-wrap items-center gap-2.5 flex-1 min-w-[280px]">
          <!-- 搜索输入框 -->
          <div class="relative w-full sm:w-64">
            <Input
              v-model:value="searchKeyword"
              allow-clear
              :placeholder="$t('ai.algorithm.searchPlaceholder')"
              @input="handleSearchInput"
              @press-enter="loadData(1, pageSize)"
            >
              <template #prefix>
                <IconifyIcon
                  icon="lucide:search"
                  class="size-4 text-muted-foreground"
                />
              </template>
            </Input>
          </div>

          <!-- 算法类型选择 -->
          <Select
            v-model:value="selectedType"
            class="w-36"
            @change="handleTypeChange"
          >
            <Select.Option value="all">
              {{ $t('ai.algorithm.allTypes') }}
            </Select.Option>
            <Select.Option value="object_detection">
              {{ $t('ai.algorithm.typeDetection') }}
            </Select.Option>
            <Select.Option value="face_recognition">
              {{ $t('ai.algorithm.typeFace') }}
            </Select.Option>
            <Select.Option value="license_plate_recognition">
              {{ $t('ai.algorithm.typePlate') }}
            </Select.Option>
          </Select>

          <!-- 来源类型过滤 -->
          <Radio.Group
            v-model:value="selectedSource"
            button-style="solid"
            @change="handleSourceChange"
          >
            <Radio.Button value="all">
              {{ $t('ai.algorithm.allSources') }}
            </Radio.Button>
            <Radio.Button value="builtin">
              {{ $t('ai.algorithm.sourceBuiltin') }}
            </Radio.Button>
            <Radio.Button value="custom">
              {{ $t('ai.algorithm.sourceCustom') }}
            </Radio.Button>
          </Radio.Group>

          <!-- 刷新按钮 -->
          <Tooltip :title="$t('ai.algorithm.refresh')">
            <Button
              class="flex items-center justify-center p-2"
              :loading="loading"
              @click="loadData(currentPage, pageSize)"
            >
              <IconifyIcon icon="lucide:refresh-cw" class="size-4" />
            </Button>
          </Tooltip>
        </div>

        <!-- 右侧主操作按钮 -->
        <div>
          <Button
            type="primary"
            class="flex items-center gap-1.5 shadow-xs"
            @click="uploadModalOpen = true"
          >
            <IconifyIcon icon="lucide:upload" class="size-4" />
            <span>{{ $t('ai.algorithm.upload') }}</span>
          </Button>
        </div>
      </div>

      <!-- 3. 卡片列表 (Responsive Grid View) -->
      <div v-if="loading && displayList.length === 0" class="py-2">
        <div class="grid grid-cols-[repeat(auto-fill,minmax(290px,1fr))] gap-4">
          <div
            v-for="i in 8"
            :key="i"
            class="rounded-2xl border border-border bg-card p-5 shadow-xs"
          >
            <Skeleton active avatar :paragraph="{ rows: 3 }" />
          </div>
        </div>
      </div>

      <div v-else-if="displayList.length > 0">
        <div class="grid grid-cols-[repeat(auto-fill,minmax(290px,1fr))] gap-4">
          <div
            v-for="algo in displayList"
            :key="algo.id"
            class="group relative flex flex-col justify-between overflow-hidden rounded-2xl border border-border bg-card p-5 shadow-xs transition-all duration-300 hover:shadow-xl hover:-translate-y-1 active:scale-[0.99] cursor-pointer h-full"
            :class="getCategoryInfo(algo.algorithmType).borderHover"
            @click="handleOpenVersions(algo)"
          >
            <!-- 顶部高光背景装饰 -->
            <div
              class="absolute -top-16 -right-16 size-32 rounded-full blur-2xl opacity-20 pointer-events-none transition-opacity group-hover:opacity-40"
              :class="getCategoryInfo(algo.algorithmType).bg"
            ></div>

            <!-- 卡片主体内容 -->
            <div>
              <!-- 卡片 Header：图标 + 标题 + 来源 Badge -->
              <div class="flex items-start justify-between gap-3">
                <div class="flex items-center gap-3 min-w-0">
                  <!-- 分类专属渐变 Avatar -->
                  <div
                    class="flex size-11 shrink-0 items-center justify-center rounded-xl shadow-xs transition-transform group-hover:scale-105"
                    :class="[
                      getCategoryInfo(algo.algorithmType).bg,
                      getCategoryInfo(algo.algorithmType).color,
                    ]"
                  >
                    <IconifyIcon
                      :icon="getCategoryInfo(algo.algorithmType).icon"
                      class="size-6"
                    />
                  </div>

                  <!-- 算法名称与 ID -->
                  <div class="min-w-0 flex-1">
                    <div class="truncate text-base font-bold text-foreground">
                      {{ formatAlgorithmName(algo.algorithmId, algo.name) }}
                    </div>
                    <div class="mt-0.5 flex items-center gap-1.5">
                      <span
                        class="truncate font-mono text-xs text-muted-foreground hover:text-foreground cursor-pointer transition-colors"
                        :title="$t('ai.algorithm.copyId')"
                        @click.stop="handleCopyId(algo.algorithmId)"
                      >
                        {{ algo.algorithmId }}
                      </span>
                      <Tooltip :title="$t('ai.algorithm.copyId')">
                        <button
                          type="button"
                          class="text-muted-foreground hover:text-foreground p-0.5"
                          @click.stop="handleCopyId(algo.algorithmId)"
                        >
                          <IconifyIcon icon="lucide:copy" class="size-3" />
                        </button>
                      </Tooltip>
                    </div>
                  </div>
                </div>

                <!-- 来源与状态 Tag -->
                <div class="shrink-0 flex items-center gap-1.5">
                  <Tag
                    v-if="algo.isBuiltin"
                    color="purple"
                    class="m-0 rounded-full text-[11px] font-medium"
                  >
                    {{ $t('ai.algorithm.builtin') }}
                  </Tag>
                  <Tag
                    v-else
                    color="blue"
                    class="m-0 rounded-full text-[11px] font-medium"
                  >
                    {{ $t('ai.algorithm.sourceCustom') }}
                  </Tag>
                  <IconifyIcon
                    icon="lucide:arrow-up-right"
                    class="size-4 text-muted-foreground/40 opacity-0 transition-all duration-200 group-hover:opacity-100 group-hover:text-primary group-hover:translate-x-0.5 group-hover:-translate-y-0.5"
                  />
                </div>
              </div>

              <!-- 激活版本与状态条 -->
              <div
                class="mt-3.5 flex items-center justify-between rounded-lg bg-muted/50 px-3 py-2 text-xs"
              >
                <div class="flex items-center gap-1.5">
                  <span
                    v-if="algo.activeVersion"
                    class="flex items-center gap-1.5 font-mono font-bold text-emerald-600 dark:text-emerald-400"
                  >
                    <span
                      class="size-2 rounded-full bg-emerald-500 animate-pulse"
                    ></span>
                    v{{ algo.activeVersion }}
                  </span>
                  <span v-else class="text-muted-foreground">
                    {{ $t('ai.algorithm.inactiveModel') }}
                  </span>
                </div>

                <div
                  class="flex items-center gap-1 font-mono text-muted-foreground text-[11px]"
                >
                  <IconifyIcon icon="lucide:layers" class="size-3.5" />
                  <span>{{ algo.versions?.length || 1 }}
                    {{ $t('ai.algorithm.installedVersions') }}</span>
                </div>
              </div>

              <!-- 描述文本 -->
              <p
                class="mt-3 text-xs leading-relaxed text-muted-foreground line-clamp-3"
                :title="formatAlgorithmDesc(algo.algorithmId, algo.description)"
              >
                {{
                  formatAlgorithmDesc(algo.algorithmId, algo.description) || '-'
                }}
              </p>

              <!-- 告警类型与特性标签 -->
              <div class="mt-3 flex flex-wrap gap-1.5">
                <Tag
                  :class="getCategoryInfo(algo.algorithmType).badgeBg"
                  class="m-0 border-0 text-xs font-medium"
                >
                  {{ getCategoryInfo(algo.algorithmType).label }}
                </Tag>
                <Tag v-if="algo.alarmTypeId" color="blue" class="m-0 text-xs">
                  <span class="flex items-center gap-1">
                    <IconifyIcon icon="lucide:bell" class="size-3" />
                    {{ formatAlarmTypeName(algo.alarmTypeId) }}
                  </span>
                </Tag>
              </div>

              <!-- 支持硬件架构 Tags -->
              <div
                v-if="getSupportedPlatforms(algo).length > 0"
                class="mt-3 flex flex-wrap items-center gap-1"
              >
                <Tag
                  v-for="platform in getSupportedPlatforms(algo)"
                  :key="platform"
                  class="m-0 flex items-center gap-1 border-border bg-background font-mono text-[11px] text-muted-foreground"
                >
                  <IconifyIcon icon="lucide:cpu" class="size-3 text-sky-500" />
                  <span>{{ platform }}</span>
                </Tag>
              </div>

              <!-- 算力 FPS 档位展示 -->
              <div
                v-if="getActiveFPSTiers(algo).length > 0"
                class="mt-2.5 flex flex-wrap items-center gap-1"
              >
                <Tag
                  v-for="tier in getActiveFPSTiers(algo)"
                  :key="`${tier.fps}-${tier.units}`"
                  class="m-0 flex items-center gap-1 border-0 bg-amber-500/10 dark:bg-amber-500/20 font-mono text-[11px] text-amber-700 dark:text-amber-300"
                >
                  <IconifyIcon
                    icon="lucide:zap"
                    class="size-3 text-amber-500"
                  />
                  <span>{{ tier.fps }} FPS</span>
                  <span class="text-muted-foreground text-[10px]">({{ tier.units }} CU)</span>
                </Tag>
              </div>
            </div>

            <!-- 卡片底部操作栏 -->
            <div class="mt-4 border-t border-border pt-3">
              <div class="flex items-center justify-between">
                <span class="font-mono text-[11px] text-muted-foreground">
                  {{ formatDateTime(algo.createdAt) || '-' }}
                </span>

                <div class="flex items-center gap-1">
                  <Button
                    type="text"
                    size="small"
                    class="flex items-center gap-1 text-xs text-muted-foreground hover:text-primary"
                    @click.stop="handleOpenActiveSchema(algo)"
                  >
                    <IconifyIcon icon="lucide:file-code-2" class="size-3.5" />
                    <span>{{ $t('ai.algorithm.schemaBtn') }}</span>
                  </Button>

                  <Button
                    type="text"
                    size="small"
                    class="flex items-center gap-1 text-xs text-primary font-medium"
                    @click.stop="handleOpenVersions(algo)"
                  >
                    <IconifyIcon icon="lucide:layers" class="size-3.5" />
                    <span>{{ $t('ai.algorithm.versionsBtn') }}</span>
                  </Button>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- 分页栏 -->
        <div
          class="mt-6 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border bg-card p-3 shadow-xs"
        >
          <div class="text-xs text-muted-foreground font-mono">
            {{ $t('ai.algorithm.totalCount', { total }) }}
          </div>
          <Pagination
            :current="currentPage"
            :page-size="pageSize"
            :total="total"
            :show-size-changer="true"
            :page-size-options="['8', '12', '24', '48']"
            size="small"
            @change="(p: number, s: number) => loadData(p, s)"
          />
        </div>
      </div>

      <!-- 4. 空数据状态 -->
      <div
        v-else
        class="flex flex-col items-center justify-center rounded-2xl border border-dashed border-border bg-card py-16 text-center"
      >
        <div
          class="flex size-14 items-center justify-center rounded-2xl bg-muted text-muted-foreground shadow-xs mb-3"
        >
          <IconifyIcon icon="lucide:inbox" class="size-7" />
        </div>
        <div class="text-base font-semibold text-foreground">
          {{ $t('ai.algorithm.emptyTitle') }}
        </div>
        <p class="text-xs text-muted-foreground mt-1 max-w-sm">
          {{ $t('ai.algorithm.emptyDesc') }}
        </p>
        <div class="mt-4">
          <Button
            type="primary"
            ghost
            class="flex items-center gap-1 text-xs"
            @click="handleResetFilter"
          >
            <IconifyIcon icon="lucide:rotate-ccw" class="size-3.5" />
            <span>{{ $t('ai.algorithm.resetFilter') }}</span>
          </Button>
        </div>
      </div>
    </div>

    <!-- 弹窗与抽屉 -->
    <UploadModal
      v-model:open="uploadModalOpen"
      @success="handleUploadSuccess"
    />
    <VersionsDrawer
      v-model:open="versionsDrawerOpen"
      :algorithm="currentSelectedAlgo"
      @refresh="handleDrawerRefresh"
      @view-schema="handleViewSchemaFromDrawer"
    />
    <SchemaModal
      v-model:open="schemaModalOpen"
      :schema="currentSchema"
      :title="currentSchemaTitle"
    />
  </Page>
</template>
