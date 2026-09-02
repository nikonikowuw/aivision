<script lang="ts" setup>
import type { FaceCaptureApi } from '#/api';

import { computed, onBeforeUnmount, ref, watch } from 'vue';

import { $t } from '@vben/locales';
import { useAccessStore } from '@vben/stores';
import { formatDateTime } from '@vben/utils';

import {
  Badge,
  Button,
  Card,
  Descriptions,
  DescriptionsItem,
  Drawer,
  Empty,
  Form,
  FormItem,
  Input,
  message,
  Modal,
  Spin,
  Tag,
} from 'ant-design-vue';

import { createPersonApi, registerPersonFaceApi } from '#/api';

import CaptureThumbnail from './CaptureThumbnail.vue';
import FaceCandidatesTable from './FaceCandidatesTable.vue';

const props = defineProps<{
  capture: FaceCaptureApi.FaceCaptureItem | null;
  open: boolean;
}>();

const emit = defineEmits<{
  (e: 'update:open', val: boolean): void;
  (e: 'registerSuccess'): void;
}>();

const accessStore = useAccessStore();
const activeIndex = ref<number>(0);
const registerModalOpen = ref<boolean>(false);
const registering = ref<boolean>(false);
const registerName = ref<string>('');
const registerPersonId = ref<string>('');

const currentSnapshots = computed(() => {
  return props.capture?.snapshots ?? [];
});

const activeSnapshot = computed(() => {
  const list = currentSnapshots.value;
  if (list.length === 0) return null;
  return list[activeIndex.value] ?? list[0] ?? null;
});

const activeCandidates = computed(() => {
  return activeSnapshot.value?.candidates ?? [];
});

const activeBbox = computed<[number, number, number, number] | null>(() => {
  const box = activeSnapshot.value?.faceBbox || activeSnapshot.value?.bbox;
  if (Array.isArray(box) && box.length === 4) {
    return box as [number, number, number, number];
  }
  return null;
});

// 计算相对首帧的时间偏移 (例如: +0.0s, +0.8s)
function getRelativeTime(index: number): string {
  const list = currentSnapshots.value;
  if (list.length === 0 || !list[0]) return '+0.0s';
  const baseNs = list[0].wallTimeNs;
  const currentNs = list[index]?.wallTimeNs || baseNs;
  const diffSec = Math.max(0, (currentNs - baseNs) / 1e9);
  return `+${diffSec.toFixed(1)}s`;
}

// 全景大图 blob 加载
const panoramaLoading = ref<boolean>(false);
const panoramaSrc = ref<string>('');
let panoObjectUrlToRevoke = '';

function cleanupPanoUrl() {
  if (panoObjectUrlToRevoke) {
    URL.revokeObjectURL(panoObjectUrlToRevoke);
    panoObjectUrlToRevoke = '';
  }
}

onBeforeUnmount(() => {
  cleanupPanoUrl();
});

watch(
  () => activeSnapshot.value?.panoramaImageUrl,
  async (url) => {
    cleanupPanoUrl();
    if (!url) {
      panoramaSrc.value = '';
      return;
    }
    panoramaLoading.value = true;
    try {
      const resp = await fetch(url, {
        headers: {
          Authorization: `Bearer ${accessStore.accessToken || ''}`,
        },
      });
      if (resp.ok) {
        const blob = await resp.blob();
        const objectUrl = URL.createObjectURL(blob);
        panoObjectUrlToRevoke = objectUrl;
        panoramaSrc.value = objectUrl;
      } else {
        const token = accessStore.accessToken;
        const delimiter = url.includes('?') ? '&' : '?';
        panoramaSrc.value = token
          ? `${url}${delimiter}token=${encodeURIComponent(token)}`
          : url;
      }
    } catch {
      panoramaSrc.value = url;
    } finally {
      panoramaLoading.value = false;
    }
  },
  { immediate: true },
);

// 重置选中项
watch(
  () => props.open,
  (val) => {
    if (val && props.capture) {
      // 默认选中最佳快照（或第一帧）
      activeIndex.value = 0;
    }
  },
);

function handleClose() {
  emit('update:open', false);
}

function openRegisterModal() {
  registerName.value = '';
  registerPersonId.value = '';
  registerModalOpen.value = true;
}

async function handleConfirmRegister() {
  const name = registerName.value.trim();
  if (!name) {
    message.warning($t('record.capture.drawer.registerNamePlaceholder'));
    return;
  }
  const snap = activeSnapshot.value;
  if (!snap || !snap.faceImageUrl) {
    message.error($t('record.capture.drawer.getFaceImageFailed'));
    return;
  }

  registering.value = true;
  try {
    // 1. 创建人员
    const personId = registerPersonId.value.trim() || undefined;
    const person = await createPersonApi({ name, personId });

    // 2. 从 faceImageUrl 获取图片 Blob 并转换为 File
    const resp = await fetch(snap.faceImageUrl, {
      headers: {
        Authorization: `Bearer ${accessStore.accessToken || ''}`,
      },
    });
    if (!resp.ok) {
      throw new Error(`HTTP ${resp.status}`);
    }
    const blob = await resp.blob();
    const file = new File([blob], `face_capture_${snap.snapshotIndex}.jpg`, {
      type: 'image/jpeg',
    });

    // 3. 注册样本人脸
    await registerPersonFaceApi(person.personId, file);

    message.success($t('record.capture.drawer.registerSuccess'));
    registerModalOpen.value = false;
    emit('registerSuccess');
  } catch (error: any) {
    message.error(error?.message || $t('record.capture.drawer.registerFailed'));
  } finally {
    registering.value = false;
  }
}
</script>

<template>
  <Drawer
    :open="open"
    :title="$t('record.capture.drawer.title')"
    width="920"
    @close="handleClose"
  >
    <div v-if="capture" class="flex flex-col gap-4">
      <!-- 头部概览卡片 -->
      <Card size="small" class="bg-neutral-50 dark:bg-neutral-900 border">
        <Descriptions size="small" :column="{ xs: 1, sm: 2, md: 4 }">
          <DescriptionsItem :label="$t('record.capture.drawer.trackId')">
            <Tag color="blue">#{{ capture.trackId }}</Tag>
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.capture.drawer.camera')">
            {{ capture.cameraName || capture.cameraId }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.capture.drawer.snapshotCount')">
            <Badge
              :count="capture.snapshotCount"
              :number-style="{ backgroundColor: '#52c41a' }"
            />
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.capture.drawer.bestSimilarity')">
            <Tag v-if="capture.bestSimilarity >= 0.8" color="green">
              {{ (capture.bestSimilarity * 100).toFixed(1) }}%
            </Tag>
            <Tag v-else-if="capture.bestSimilarity > 0" color="orange">
              {{ (capture.bestSimilarity * 100).toFixed(1) }}%
            </Tag>
            <Tag v-else color="default">
              {{ $t('record.capture.drawer.stranger') }}
            </Tag>
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.capture.drawer.bestPerson')">
            <span
              v-if="capture.bestPersonName"
              class="font-medium text-primary"
            >
              {{ capture.bestPersonName }}
            </span>
            <span v-else class="text-neutral-400">
              {{ $t('record.capture.drawer.stranger') }}
            </span>
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.capture.drawer.startTime')">
            {{ formatDateTime(capture.firstObservedAt) || '-' }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.capture.drawer.endTime')">
            {{ formatDateTime(capture.lastObservedAt) || '-' }}
          </DescriptionsItem>
          <DescriptionsItem :label="$t('record.capture.drawer.algorithm')">
            <span class="text-xs text-neutral-500">
              {{ capture.algorithmId }} (v{{ capture.algorithmVersion }})
            </span>
          </DescriptionsItem>
        </Descriptions>
      </Card>

      <!-- 当前选中帧的主视图（全景 + 人脸框定位 + 右侧特写对比） -->
      <div v-if="activeSnapshot" class="grid grid-cols-1 md:grid-cols-3 gap-4">
        <!-- 左侧 2 栏：全景定位图（带 SVG 边界框） -->
        <Card
          size="small"
          class="md:col-span-2 overflow-hidden flex flex-col justify-center items-center relative min-h-[320px] bg-black/5 dark:bg-black/40"
        >
          <template #title>
            <div class="flex items-center justify-between text-xs">
              <span class="font-medium">
                {{ $t('record.capture.drawer.panoramaLocation') }} ({{
                  $t('record.capture.drawer.snapshotIndex', {
                    index: activeSnapshot.snapshotIndex,
                  })
                }})
              </span>
              <span class="text-neutral-400">
                {{ $t('record.capture.drawer.snapshotTime') }}:
                {{
                  formatDateTime(
                    activeSnapshot.capturedAt || activeSnapshot.observedAt,
                  ) || '-'
                }}
              </span>
            </div>
          </template>

          <div class="relative w-full h-full flex items-center justify-center">
            <Spin v-if="panoramaLoading" />
            <div
              v-else-if="panoramaSrc"
              class="relative inline-block max-w-full max-h-[360px]"
            >
              <img
                :src="panoramaSrc"
                alt="Panorama"
                class="max-h-[360px] w-auto object-contain rounded select-none block"
              />
              <!-- SVG 动态人脸边界框覆盖 -->
              <svg
                v-if="activeBbox"
                viewBox="0 0 100 100"
                preserveAspectRatio="none"
                class="absolute inset-0 w-full h-full pointer-events-none"
              >
                <rect
                  :x="activeBbox[0] * 100"
                  :y="activeBbox[1] * 100"
                  :width="(activeBbox[2] - activeBbox[0]) * 100"
                  :height="(activeBbox[3] - activeBbox[1]) * 100"
                  fill="rgba(24, 144, 255, 0.25)"
                  stroke="#1890ff"
                  stroke-width="1.2"
                  stroke-dasharray="2,1"
                />
              </svg>
            </div>
            <Empty
              v-else
              :description="$t('record.capture.drawer.noPanorama')"
            />
          </div>
        </Card>

        <!-- 右侧 1 栏：当前帧人脸特写与识别属性 -->
        <Card size="small" class="flex flex-col justify-between">
          <template #title>
            <span class="text-xs font-medium">{{
              $t('record.capture.drawer.currentPreview')
            }}</span>
          </template>

          <div class="flex flex-col items-center gap-3 py-2">
            <CaptureThumbnail
              :url="activeSnapshot.faceImageUrl"
              :width="128"
              :height="128"
              :original="true"
              fit="contain"
              class="border-2 border-primary/40 rounded-lg shadow-sm"
            />

            <div class="w-full flex flex-col gap-2 text-xs">
              <div class="flex justify-between items-center py-1 border-b">
                <span class="text-neutral-500">{{ $t('record.capture.drawer.frameSeq') }}:</span>
                <Tag color="cyan">
                  #{{ activeSnapshot.snapshotIndex }} /
                  {{ currentSnapshots.length }}
                </Tag>
              </div>
              <div class="flex justify-between items-center py-1 border-b">
                <span class="text-neutral-500">{{ $t('record.capture.drawer.qualityScore') }}:</span>
                <span class="font-bold text-emerald-600">
                  {{ (activeSnapshot.qualityScore * 100).toFixed(1) }}%
                </span>
              </div>
              <div class="flex justify-between items-center py-1 border-b">
                <span class="text-neutral-500">{{ $t('record.capture.drawer.similarity') }}:</span>
                <Tag v-if="activeSnapshot.similarity >= 0.8" color="green">
                  {{ (activeSnapshot.similarity * 100).toFixed(1) }}%
                </Tag>
                <Tag v-else-if="activeSnapshot.similarity > 0" color="orange">
                  {{ (activeSnapshot.similarity * 100).toFixed(1) }}%
                </Tag>
                <Tag v-else color="default">
                  {{ $t('record.capture.drawer.stranger') }}
                </Tag>
              </div>
              <div class="flex justify-between items-center py-1 border-b">
                <span class="text-neutral-500">{{ $t('record.capture.drawer.matchedPerson') }}:</span>
                <span
                  class="font-medium text-neutral-800 dark:text-neutral-200"
                >
                  {{
                    activeSnapshot.personName ||
                    $t('record.capture.drawer.stranger')
                  }}
                </span>
              </div>
            </div>

            <!-- 陌生人快捷注册入口 -->
            <Button
              v-if="!activeSnapshot.personId"
              type="primary"
              size="small"
              class="w-full mt-2"
              @click="openRegisterModal"
            >
              {{ $t('record.capture.drawer.quickRegister') }}
            </Button>
          </div>
        </Card>
      </div>

      <!-- Top-5 候选底库比对分析卡片 -->
      <Card size="small" class="border bg-white dark:bg-neutral-900">
        <template #title>
          <div class="flex items-center justify-between">
            <span
              class="font-semibold text-xs text-neutral-700 dark:text-neutral-300"
            >
              {{ $t('record.capture.drawer.candidatesTitle') }} ({{
                $t('record.capture.drawer.snapshotIndex', {
                  index: activeSnapshot?.snapshotIndex || 1,
                })
              }})
            </span>
            <span class="text-xs text-neutral-400">
              {{
                activeCandidates.length > 0
                  ? `${activeCandidates.length} 条底库候选`
                  : $t('record.capture.drawer.noCandidates')
              }}
            </span>
          </div>
        </template>

        <FaceCandidatesTable
          :candidates="activeCandidates"
          :show-match-badge="true"
          :match-threshold="0.7"
        />
      </Card>

      <!-- 底部时序胶卷序列 Filmstrip -->
      <Card size="small" class="border">
        <template #title>
          <div class="flex items-center justify-between">
            <span
              class="font-semibold text-xs text-neutral-700 dark:text-neutral-300"
            >
              🎞️ {{ $t('record.capture.drawer.filmstrip') }}
            </span>
            <span class="text-xs text-neutral-400">
              {{ $t('record.capture.drawer.selectTip') }}
            </span>
          </div>
        </template>

        <div class="flex items-center gap-3 overflow-x-auto py-2 px-1">
          <div
            v-for="(snap, index) in currentSnapshots"
            :key="snap.snapshotIndex"
            class="flex flex-col items-center gap-1 p-2 rounded-lg border-2 cursor-pointer transition-all min-w-[120px] bg-white dark:bg-neutral-800 hover:shadow-md"
            :class="[
              activeIndex === index
                ? 'border-primary shadow-md scale-105 ring-2 ring-primary/20'
                : 'border-neutral-200 dark:border-neutral-700 opacity-80 hover:opacity-100',
            ]"
            @click="activeIndex = index"
          >
            <div
              class="flex items-center justify-between w-full text-[11px] font-medium text-neutral-500"
            >
              <span>#{{ snap.snapshotIndex }}</span>
              <span class="text-primary font-mono">{{
                getRelativeTime(index)
              }}</span>
            </div>

            <CaptureThumbnail
              :url="snap.faceImageUrl"
              :width="72"
              :height="72"
              fit="cover"
              :preview="false"
              class="rounded border"
            />

            <div
              class="w-full flex items-center justify-between text-[10px] text-neutral-400 mt-1"
            >
              <span>{{ $t('record.capture.drawer.quality') }}:
                {{ (snap.qualityScore * 100).toFixed(0) }}%</span>
              <span
                v-if="snap.similarity > 0"
                class="text-green-600 font-medium"
              >
                {{ (snap.similarity * 100).toFixed(0) }}%
              </span>
              <span v-else class="text-neutral-400">{{
                $t('record.capture.drawer.stranger')
              }}</span>
            </div>
          </div>
        </div>
      </Card>
    </div>

    <!-- 快捷注册模态框 -->
    <Modal
      v-model:open="registerModalOpen"
      :title="$t('record.capture.drawer.quickRegister')"
      :confirm-loading="registering"
      @ok="handleConfirmRegister"
    >
      <div class="flex flex-col gap-4 py-2">
        <div
          class="flex items-center gap-4 p-3 bg-neutral-50 dark:bg-neutral-900 rounded border"
        >
          <CaptureThumbnail
            v-if="activeSnapshot"
            :url="activeSnapshot.faceImageUrl"
            :width="64"
            :height="64"
            :original="true"
            fit="cover"
            :preview="false"
            class="rounded border"
          />
          <div class="flex flex-col text-xs text-neutral-500">
            <span>{{
              $t('record.capture.drawer.registerTip', {
                index: activeSnapshot?.snapshotIndex,
              })
            }}</span>
          </div>
        </div>

        <Form layout="vertical">
          <FormItem :label="$t('record.capture.drawer.registerName')" required>
            <Input
              v-model:value="registerName"
              :placeholder="$t('record.capture.drawer.registerNamePlaceholder')"
              :maxlength="32"
            />
          </FormItem>
          <FormItem :label="$t('record.capture.drawer.registerPersonId')">
            <Input
              v-model:value="registerPersonId"
              :placeholder="
                $t('record.capture.drawer.registerPersonIdPlaceholder')
              "
              :maxlength="64"
            />
          </FormItem>
        </Form>
      </div>
    </Modal>
  </Drawer>
</template>
