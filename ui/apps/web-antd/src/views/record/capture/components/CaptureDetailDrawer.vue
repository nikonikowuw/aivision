<script lang="ts" setup>
import type { CaptureApi } from '#/api';

import { computed, ref } from 'vue';

import { $t } from '@vben/locales';
import { useAccessStore } from '@vben/stores';
import { formatDateTime } from '@vben/utils';

import {
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
  Space,
  Tag,
} from 'ant-design-vue';

import { createPersonApi, registerPersonFaceApi } from '#/api';

import CaptureThumbnail from './CaptureThumbnail.vue';

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

const attributeEntries = computed(() =>
  Object.entries(props.capture?.attributes ?? {}).filter(
    ([, value]) => value !== undefined && value !== null,
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

function targetLabel(targetType?: string): string {
  if (!targetType) return '-';
  return $t(`record.capture.targets.${targetType}`);
}

function formatAttribute(value: unknown): string {
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value);
  }
  try {
    return JSON.stringify(value);
  } catch {
    return '-';
  }
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
      <Descriptions bordered size="small" :column="{ xs: 1, sm: 2, md: 3 }">
        <DescriptionsItem :label="$t('record.capture.drawer.targetType')">
          <Tag color="cyan">{{ targetLabel(capture.targetType) }}</Tag>
        </DescriptionsItem>
        <DescriptionsItem :label="$t('record.capture.drawer.eventId')">
          <span class="font-mono text-xs break-all">{{ capture.eventId }}</span>
        </DescriptionsItem>
        <DescriptionsItem :label="$t('record.capture.drawer.trackId')">
          {{ capture.trackId ? `#${capture.trackId}` : '-' }}
        </DescriptionsItem>
        <DescriptionsItem :label="$t('record.capture.drawer.camera')">
          {{ capture.cameraName || capture.cameraId || '-' }}
        </DescriptionsItem>
        <DescriptionsItem :label="$t('record.capture.drawer.capturedAt')">
          {{ formatDateTime(capture.capturedAt) || '-' }}
        </DescriptionsItem>
        <DescriptionsItem :label="$t('record.capture.drawer.recognized')">
          <Tag :color="capture.isRecognized ? 'green' : 'default'">
            {{
              capture.isRecognized
                ? $t('record.capture.advanced.recognizedYes')
                : $t('record.capture.advanced.recognizedNo')
            }}
          </Tag>
        </DescriptionsItem>
        <DescriptionsItem :label="$t('record.capture.drawer.confidence')">
          {{ formatRatio(capture.confidence) }}
        </DescriptionsItem>
        <DescriptionsItem :label="$t('record.capture.drawer.quality')">
          {{ formatRatio(capture.qualityScore) }}
        </DescriptionsItem>
        <DescriptionsItem :label="$t('record.capture.drawer.algorithm')">
          {{ capture.algorithmId }} v{{ capture.algorithmVersion }}
        </DescriptionsItem>
      </Descriptions>

      <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
        <Card size="small" :title="$t('record.capture.drawer.panorama')">
          <CaptureThumbnail
            :alt="$t('record.capture.drawer.imageCleaned')"
            :bbox="capture.bbox"
            class="mx-auto"
            fit="contain"
            :height="240"
            :original="true"
            :preview="true"
            :url="capture.imageUrl"
            :width="320"
          />
        </Card>
        <Card size="small" :title="$t('record.capture.drawer.primaryCrop')">
          <CaptureThumbnail
            :alt="$t('record.capture.drawer.imageCleaned')"
            class="mx-auto"
            fit="contain"
            :height="240"
            :original="true"
            :url="capture.cropImageUrl"
            :width="240"
          />
        </Card>
        <Card size="small" :title="$t('record.capture.drawer.subCrop')">
          <CaptureThumbnail
            :alt="$t('record.capture.drawer.imageCleaned')"
            :bbox="capture.subBbox"
            class="mx-auto"
            fit="contain"
            :height="240"
            :original="true"
            :url="capture.subCropImageUrl"
            :width="240"
          />
        </Card>
      </div>

      <Card
        v-if="attributeEntries.length > 0"
        size="small"
        :title="$t('record.capture.drawer.attributes')"
      >
        <div class="grid grid-cols-1 gap-x-6 gap-y-2 text-sm md:grid-cols-2">
          <div
            v-for="[key, value] in attributeEntries"
            :key="key"
            class="flex min-w-0 items-start justify-between gap-3 border-b border-neutral-100 py-1.5 dark:border-neutral-800"
          >
            <span class="shrink-0 text-neutral-500">{{ key }}</span>
            <span class="break-all text-right">{{
              formatAttribute(value)
            }}</span>
          </div>
        </div>
      </Card>

      <div v-if="canRegisterFace" class="flex justify-end">
        <Button type="primary" @click="openRegisterModal">
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
        class="mb-4 flex items-center gap-4 rounded border bg-neutral-50 p-3 dark:bg-neutral-900"
      >
        <CaptureThumbnail
          fit="cover"
          :height="64"
          :original="true"
          :preview="false"
          :url="registerImageUrl"
          :width="64"
          class="rounded border"
        />
        <div class="flex flex-col text-xs text-neutral-500">
          <span class="font-medium text-neutral-700 dark:text-neutral-300">
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
