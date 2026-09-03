<script lang="ts" setup>
import type { CaptureApi } from '#/api';

import { computed, ref } from 'vue';

import { IconifyIcon } from '@vben/icons';
import { $t } from '@vben/locales';
import { useAccessStore } from '@vben/stores';
import { formatDateTime } from '@vben/utils';

import {
  Button,
  Card,
  Drawer,
  Empty,
  Form,
  FormItem,
  Input,
  message,
  Modal,
  Space,
  Tag,
  Tooltip,
} from 'ant-design-vue';

import { createPersonApi, registerPersonFaceApi } from '#/api';
import { copyToClipboard } from '#/utils/clipboard';
import { getConfidenceTagColor } from '#/utils/format';

import CaptureThumbnail from './CaptureThumbnail.vue';
import FaceCandidatesTable, {
  type FaceCandidateItem,
} from './FaceCandidatesTable.vue';

const props = defineProps<{
  capture: CaptureApi.CaptureItem | null;
  open: boolean;
}>();

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void;
  (e: 'registerSuccess'): void;
}>();

const accessStore = useAccessStore();
const registerModalOpen = ref(false);
const registering = ref(false);
const registerName = ref('');
const registerPersonId = ref('');

const hasSubCrop = computed(() => Boolean(props.capture?.subCropImageUrl));

const ignoredAttributeKeys = new Set([
  'bbox',
  'face_bbox',
  'sub_bbox',
  'person_bbox',
  'landmarks',
  'face_landmarks',
  'embedding',
  'person_confidence',
  'face_confidence',
  'has_face',
  'candidates',
  'match',
]);

const faceCandidates = computed(() => {
  const cands = props.capture?.attributes?.candidates;
  return Array.isArray(cands) ? (cands as FaceCandidateItem[]) : [];
});

const attributeEntries = computed(() =>
  Object.entries(props.capture?.attributes ?? {}).filter(
    ([key, value]) =>
      !ignoredAttributeKeys.has(key) &&
      value !== undefined &&
      value !== null &&
      value !== '',
  ),
);

const registerImageUrl = computed(() => {
  if (!props.capture) return '';
  if (props.capture.targetType === 'face') {
    return props.capture.cropImageUrl || '';
  }
  if (props.capture.targetType === 'person') {
    return props.capture.subCropImageUrl || '';
  }
  return '';
});

const canRegisterFace = computed(() => {
  if (!props.capture || props.capture.isRecognized) return false;
  return Boolean(registerImageUrl.value);
});

function getTargetIcon(targetType?: string): string {
  switch (targetType) {
    case 'face':
      return 'lucide:smile';
    case 'person':
      return 'lucide:user';
    case 'vehicle':
      return 'lucide:car';
    case 'non_motor':
      return 'lucide:bike';
    default:
      return 'lucide:box';
  }
}

function targetLabel(targetType?: string): string {
  if (!targetType) return '-';
  return $t(`record.capture.targets.${targetType}`);
}

function formatAttributeKey(key: string): string {
  const map: Record<string, string> = {
    age: '年龄段',
    bottom_color: '下装颜色',
    color: '目标颜色',
    gender: '性别',
    glasses: '佩戴眼镜',
    hat: '佩戴帽子',
    helmet: '佩戴头盔',
    mask: '佩戴口罩',
    plate_number: '车牌号码',
    top_color: '上装颜色',
    vehicle_type: '车辆类型',
  };
  return map[key] || key;
}

function formatAttributeValue(val: unknown): string {
  if (typeof val === 'boolean') {
    return val ? '是' : '否';
  }
  if (typeof val === 'number') {
    if (val > 0 && val <= 1 && String(val).includes('.')) {
      return `${(val * 100).toFixed(1)}%`;
    }
    return String(val);
  }
  if (Array.isArray(val)) {
    return `[${val.map((v) => (typeof v === 'number' ? v.toFixed(3) : String(v))).join(', ')}]`;
  }
  if (typeof val === 'object' && val !== null) {
    return JSON.stringify(val);
  }
  return String(val ?? '-');
}

function formatRatio(value?: number): string {
  return typeof value === 'number' ? `${(value * 100).toFixed(1)}%` : '-';
}

function closeDrawer() {
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
  if (!registerImageUrl.value) {
    message.error($t('record.capture.drawer.imageCleaned'));
    return;
  }

  registering.value = true;
  try {
    const person = await createPersonApi({
      name,
      personId: registerPersonId.value.trim() || undefined,
    });
    const response = await fetch(registerImageUrl.value, {
      headers: {
        Authorization: `Bearer ${accessStore.accessToken || ''}`,
      },
    });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    const blob = await response.blob();
    await registerPersonFaceApi(
      person.personId,
      new File([blob], `capture_${props.capture?.id || 'face'}.jpg`, {
        type: blob.type || 'image/jpeg',
      }),
    );
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
    :width="920"
    @close="closeDrawer"
  >
    <Empty v-if="!capture" :description="$t('record.capture.drawer.noImage')" />

    <div v-else class="flex flex-col gap-4">
      <!-- 1. 顶部 Hero 感知概览横幅 -->
      <div
        class="flex flex-wrap items-center justify-between gap-4 rounded-xl border border-border/80 bg-gradient-to-r from-card via-card to-muted/20 p-4 shadow-xs"
      >
        <!-- 左侧：类型徽章与摄像头/时间 -->
        <div class="flex items-center gap-3.5 min-w-0">
          <div
            class="flex size-11 shrink-0 items-center justify-center rounded-xl border shadow-xs"
            :class="[
              capture.targetType === 'face'
                ? 'border-cyan-500/30 bg-cyan-500/10 text-cyan-600 dark:text-cyan-400'
                : capture.targetType === 'person'
                  ? 'border-blue-500/30 bg-blue-500/10 text-blue-600 dark:text-blue-400'
                  : capture.targetType === 'vehicle'
                    ? 'border-amber-500/30 bg-amber-500/10 text-amber-600 dark:text-amber-400'
                    : 'border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
            ]"
          >
            <IconifyIcon :icon="getTargetIcon(capture.targetType)" class="size-6" />
          </div>

          <div class="flex flex-col min-w-0 gap-0.5">
            <div class="flex items-center gap-2 flex-wrap">
              <span class="text-sm font-bold text-foreground">
                {{ targetLabel(capture.targetType) }}
              </span>
              <Tag
                :color="capture.isRecognized ? 'green' : 'default'"
                class="m-0 text-[11px] font-medium"
              >
                {{
                  capture.isRecognized
                    ? $t('record.capture.advanced.recognizedYes')
                    : $t('record.capture.advanced.recognizedNo')
                }}
              </Tag>
              <Tag v-if="capture.trackId" color="purple" class="m-0 font-mono text-[11px]">
                #{{ capture.trackId }}
              </Tag>
            </div>
            <div class="flex items-center gap-3 text-xs text-muted-foreground truncate flex-wrap">
              <span class="flex items-center gap-1">
                <IconifyIcon icon="lucide:video" class="size-3.5 shrink-0" />
                {{ capture.cameraName || capture.cameraId || '-' }}
              </span>
              <span class="flex items-center gap-1 font-mono">
                <IconifyIcon icon="lucide:clock" class="size-3.5 shrink-0" />
                {{ formatDateTime(capture.capturedAt) || '-' }}
              </span>
            </div>
          </div>
        </div>

        <!-- 右侧：核心量化指标 (置信度与质量) -->
        <div class="flex items-center gap-3 shrink-0 ml-auto">
          <div class="flex flex-col items-center justify-center rounded-lg border border-border/80 bg-muted/40 px-3 py-1.5 min-w-[80px]">
            <span class="text-[10px] uppercase font-semibold text-muted-foreground">{{ $t('record.capture.drawer.confidence') }}</span>
            <Tag :color="getConfidenceTagColor(capture.confidence)" class="m-0 mt-0.5 font-mono font-bold text-xs">
              {{ formatRatio(capture.confidence) }}
            </Tag>
          </div>
          <div class="flex flex-col items-center justify-center rounded-lg border border-border/80 bg-muted/40 px-3 py-1.5 min-w-[80px]">
            <span class="text-[10px] uppercase font-semibold text-muted-foreground">{{ $t('record.capture.drawer.quality') }}</span>
            <Tag :color="getConfidenceTagColor(capture.qualityScore)" class="m-0 mt-0.5 font-mono font-bold text-xs">
              {{ formatRatio(capture.qualityScore) }}
            </Tag>
          </div>
        </div>
      </div>

      <!-- 2. 视觉多重视角画廊 (自适应 2~3 列网格) -->
      <div
        class="grid gap-4"
        :class="[hasSubCrop ? 'grid-cols-1 md:grid-cols-2 lg:grid-cols-3' : 'grid-cols-1 md:grid-cols-2']"
      >
        <!-- 全景抓拍定位图 -->
        <Card size="small" class="overflow-hidden shadow-xs border-border/80">
          <template #title>
            <div class="flex items-center gap-2 text-xs font-semibold">
              <IconifyIcon icon="lucide:image" class="size-4 text-primary" />
              <span>{{ $t('record.capture.drawer.panorama') }}</span>
              <span class="text-[11px] font-normal text-muted-foreground">({{ $t('record.capture.drawer.panoramaLocation') || '全景定位' }})</span>
            </div>
          </template>
          <div class="flex items-center justify-center rounded-lg bg-neutral-950 p-2 min-h-[220px]">
            <CaptureThumbnail
              :alt="$t('record.capture.drawer.imageCleaned')"
              :bbox="capture.bbox"
              class="mx-auto"
              fit="contain"
              :height="220"
              :original="true"
              :preview="true"
              :url="capture.imageUrl"
              :width="320"
            />
          </div>
        </Card>

        <!-- 主体特写切图 -->
        <Card size="small" class="overflow-hidden shadow-xs border-border/80">
          <template #title>
            <div class="flex items-center gap-2 text-xs font-semibold">
              <IconifyIcon icon="lucide:crop" class="size-4 text-primary" />
              <span>{{ $t('record.capture.drawer.primaryCrop') }}</span>
              <span class="text-[11px] font-normal text-muted-foreground">({{ targetLabel(capture.targetType) }})</span>
            </div>
          </template>
          <div class="flex items-center justify-center rounded-lg bg-neutral-950 p-2 min-h-[220px]">
            <CaptureThumbnail
              :alt="$t('record.capture.drawer.imageCleaned')"
              class="mx-auto"
              fit="contain"
              :height="220"
              :original="true"
              :preview="true"
              :url="capture.cropImageUrl"
              :width="220"
            />
          </div>
        </Card>

        <!-- 附属特写切图 (如人脸特写，仅在存在时渲染) -->
        <Card v-if="hasSubCrop" size="small" class="overflow-hidden shadow-xs border-border/80">
          <template #title>
            <div class="flex items-center gap-2 text-xs font-semibold">
              <IconifyIcon icon="lucide:scan-face" class="size-4 text-primary" />
              <span>{{ $t('record.capture.drawer.subCrop') }}</span>
              <span class="text-[11px] font-normal text-muted-foreground">(局部人脸)</span>
            </div>
          </template>
          <div class="flex items-center justify-center rounded-lg bg-neutral-950 p-2 min-h-[220px]">
            <CaptureThumbnail
              :alt="$t('record.capture.drawer.imageCleaned')"
              :bbox="capture.subBbox"
              class="mx-auto"
              fit="contain"
              :height="220"
              :original="true"
              :preview="true"
              :url="capture.subCropImageUrl"
              :width="220"
            />
          </div>
        </Card>
      </div>

      <!-- 3. 事件与抓拍详细参数 -->
      <Card size="small" class="shadow-xs border-border/80">
        <template #title>
          <div class="flex items-center gap-2 text-xs font-semibold">
            <IconifyIcon icon="lucide:info" class="size-4 text-primary" />
            <span>{{ $t('record.capture.drawer.trackInfo') || '事件与抓拍参数' }}</span>
          </div>
        </template>

        <div class="grid grid-cols-1 gap-x-6 gap-y-3 sm:grid-cols-2 lg:grid-cols-3 text-xs">
          <!-- 抓拍摄像头 -->
          <div class="flex items-center">
            <span class="meta-label">{{ $t('record.capture.drawer.camera') }}</span>
            <span class="meta-colon">：</span>
            <span class="font-medium text-foreground break-all truncate flex-1">{{ capture.cameraName || capture.cameraId || '-' }}</span>
          </div>

          <!-- 抓拍时间 -->
          <div class="flex items-center">
            <span class="meta-label">{{ $t('record.capture.drawer.capturedAt') }}</span>
            <span class="meta-colon">：</span>
            <span class="font-mono text-foreground flex-1">{{ formatDateTime(capture.capturedAt) || '-' }}</span>
          </div>

          <!-- Track ID -->
          <div class="flex items-center">
            <span class="meta-label">{{ $t('record.capture.drawer.trackId') }}</span>
            <span class="meta-colon">：</span>
            <div class="flex-1">
              <Tag v-if="capture.trackId" color="purple" class="m-0 font-mono text-[11px]">#{{ capture.trackId }}</Tag>
              <span v-else class="text-muted-foreground">-</span>
            </div>
          </div>

          <!-- 算法模型与版本 -->
          <div class="flex items-center">
            <span class="meta-label">{{ $t('record.capture.drawer.algorithm') }}</span>
            <span class="meta-colon">：</span>
            <span class="font-mono text-foreground flex-1">{{ capture.algorithmId }} <span class="text-muted-foreground">v{{ capture.algorithmVersion }}</span></span>
          </div>

          <!-- 事件标识 (带一键复制) -->
          <div class="flex items-center sm:col-span-2 lg:col-span-2">
            <span class="meta-label">{{ $t('record.capture.drawer.eventId') }}</span>
            <span class="meta-colon">：</span>
            <div class="flex items-center gap-1.5 min-w-0 flex-1">
              <span class="font-mono text-[11px] truncate text-foreground/90" :title="capture.eventId">
                {{ capture.eventId }}
              </span>
              <Tooltip :title="$t('system.common.copy') || '复制标识'">
                <Button
                  type="text"
                  size="small"
                  class="shrink-0 p-0.5 h-6 w-6 flex items-center justify-center text-muted-foreground hover:text-primary"
                  @click="copyToClipboard(capture.eventId, $t('record.face.detail.copySuccess') || '复制成功')"
                >
                  <IconifyIcon icon="lucide:copy" class="size-3.5" />
                </Button>
              </Tooltip>
            </div>
          </div>
        </div>
      </Card>

      <!-- 4. Top-K 候选识别结果 (当抓拍存在人脸特征并完成底库比对时展示) -->
      <Card
        v-if="faceCandidates.length > 0"
        size="small"
        class="shadow-xs border-border/80"
      >
        <template #title>
          <div class="flex items-center gap-2 text-xs font-semibold">
            <IconifyIcon icon="lucide:users" class="size-4 text-primary" />
            <span>{{ $t('record.capture.drawer.candidatesTitle') || 'Top-5 候选底库比对分析' }}</span>
            <Tag color="blue" class="m-0 text-[10px] font-mono">Top-{{ faceCandidates.length }}</Tag>
          </div>
        </template>
        <FaceCandidatesTable
          :candidates="faceCandidates"
          :show-match-badge="true"
        />
      </Card>

      <!-- 5. AI 目标属性 -->
      <Card
        v-if="attributeEntries.length > 0"
        size="small"
        class="shadow-xs border-border/80"
      >
        <template #title>
          <div class="flex items-center gap-2 text-xs font-semibold">
            <IconifyIcon icon="lucide:sparkles" class="size-4 text-amber-500" />
            <span>{{ $t('record.capture.drawer.attributes') }}</span>
          </div>
        </template>

        <div class="grid grid-cols-1 gap-2.5 sm:grid-cols-2 lg:grid-cols-3">
          <div
            v-for="[key, value] in attributeEntries"
            :key="key"
            class="flex items-center justify-between gap-2 rounded-md border border-border/60 bg-muted/30 px-3 py-2 text-xs"
          >
            <span class="font-medium text-muted-foreground">{{ formatAttributeKey(key) }}</span>
            <span class="font-mono font-semibold text-foreground break-all text-right">
              {{ formatAttributeValue(value) }}
            </span>
          </div>
        </div>
      </Card>

      <!-- 6. 快捷注册区域 -->
      <div
        v-if="canRegisterFace"
        class="flex items-center justify-between rounded-xl border border-primary/20 bg-primary/5 p-3.5"
      >
        <div class="flex items-center gap-3">
          <div class="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <IconifyIcon icon="lucide:user-plus" class="size-5" />
          </div>
          <div>
            <div class="text-xs font-semibold text-foreground">
              {{ $t('record.capture.drawer.quickRegister') }}
            </div>
            <div class="text-[11px] text-muted-foreground">
              未识别的有效人脸特写，可一键将其注册至人员库底库中
            </div>
          </div>
        </div>
        <Button type="primary" @click="openRegisterModal">
          <template #icon>
            <IconifyIcon icon="lucide:user-plus" class="size-3.5 mr-1" />
          </template>
          {{ $t('record.capture.drawer.quickRegister') }}
        </Button>
      </div>
    </div>

    <Modal
      v-model:open="registerModalOpen"
      :confirm-loading="registering"
      :title="$t('record.capture.drawer.quickRegister')"
      @ok="handleConfirmRegister"
    >
      <div
        v-if="registerImageUrl"
        class="mb-4 flex items-center gap-4 rounded-xl border border-border/80 bg-muted/30 p-3"
      >
        <CaptureThumbnail
          fit="cover"
          :height="64"
          :original="true"
          :preview="false"
          :url="registerImageUrl"
          :width="64"
          class="rounded-lg border border-border/80 shadow-xs"
        />
        <div class="flex flex-col text-xs text-muted-foreground gap-1">
          <span class="font-bold text-foreground">
            {{ targetLabel(capture?.targetType) }}
          </span>
          <span>{{ capture?.cameraName || capture?.cameraId }}</span>
        </div>
      </div>
      <Form layout="vertical">
        <FormItem :label="$t('record.capture.drawer.registerName')" required>
          <Input
            v-model:value="registerName"
            :maxlength="32"
            :placeholder="$t('record.capture.drawer.registerNamePlaceholder')"
          />
        </FormItem>
        <FormItem :label="$t('record.capture.drawer.registerPersonId')">
          <Input
            v-model:value="registerPersonId"
            :maxlength="64"
            :placeholder="
              $t('record.capture.drawer.registerPersonIdPlaceholder')
            "
          />
        </FormItem>
      </Form>
      <template #footer>
        <Space>
          <Button @click="registerModalOpen = false">
            {{ $t('system.common.cancel') }}
          </Button>
          <Button
            type="primary"
            :loading="registering"
            @click="handleConfirmRegister"
          >
            {{ $t('system.common.confirm') }}
          </Button>
        </Space>
      </template>
    </Modal>
  </Drawer>
</template>

<style scoped>
.meta-label {
  display: inline-block;
  width: 64px;
  min-width: 64px;
  text-align: justify;
  text-align-last: justify;
  color: hsl(var(--muted-foreground));
  font-size: 12px;
  font-weight: 500;
  line-height: 1.5;
}

.meta-colon {
  display: inline-block;
  color: hsl(var(--muted-foreground));
  font-size: 12px;
  font-weight: 400;
  margin-left: 2px;
  margin-right: 6px;
}
</style>
