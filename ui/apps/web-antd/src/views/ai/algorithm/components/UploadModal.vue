<script lang="ts" setup>
import type { UploadProps } from 'ant-design-vue';

import { computed, ref } from 'vue';

import { $t } from '@vben/locales';

import {
  Alert,
  message,
  Modal,
  Spin,
  Steps,
  Tag,
  Upload,
} from 'ant-design-vue';

import { IconifyIcon } from '@vben/icons';

import { uploadAlgorithmPackage } from '#/api';

interface Props {
  open: boolean;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  (e: 'update:open', val: boolean): void;
  (e: 'success'): void;
}>();

const visible = computed({
  get: () => props.open,
  set: (val) => emit('update:open', val),
});

const uploading = ref(false);
const uploadStep = ref<number>(0);
const uploadStatusText = ref('');
const errorMessage = ref('');
const selectedFileName = ref('');
const selectedFileSize = ref(0);

const beforeUpload: UploadProps['beforeUpload'] = (file) => {
  const name = file.name.toLowerCase();
  const isValidArchive =
    name.endsWith('.zip') ||
    name.endsWith('.tar.gz') ||
    name.endsWith('.tgz') ||
    name.endsWith('.tar') ||
    file.type === 'application/zip' ||
    file.type === 'application/x-zip-compressed' ||
    file.type === 'application/gzip' ||
    file.type === 'application/x-gzip' ||
    file.type === 'application/x-tar';

  if (!isValidArchive) {
    message.error($t('ai.algorithm.onlySupportedFormats'));
    return false;
  }

  selectedFileName.value = file.name;
  selectedFileSize.value = file.size;
  handleUpload(file);
  return false; // 阻止 Antd 默认自动上传
};

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`;
}

async function handleUpload(file: File) {
  uploading.value = true;
  errorMessage.value = '';
  uploadStep.value = 0;
  uploadStatusText.value = $t('ai.algorithm.uploadingTip');

  try {
    uploadStep.value = 1;
    uploadStatusText.value = $t('ai.algorithm.validatingTip');
    await uploadAlgorithmPackage(file);
    uploadStep.value = 2;
    message.success($t('ai.algorithm.installSuccess'));
    emit('success');
    visible.value = false;
  } catch (error: any) {
    errorMessage.value = error?.message || $t('ai.algorithm.installFailed');
    uploadStep.value = 0;
  } finally {
    uploading.value = false;
  }
}

function handleClose() {
  if (uploading.value) return;
  visible.value = false;
}
</script>

<template>
  <Modal
    v-model:open="visible"
    :title="$t('ai.algorithm.uploadTitle')"
    :footer="null"
    width="680px"
    destroy-on-close
    @cancel="handleClose"
  >
    <div class="py-2 space-y-4">
      <!-- 规范指引说明卡片 -->
      <div
        class="rounded-xl border border-border bg-muted/40 p-3.5 text-xs text-muted-foreground"
      >
        <div class="flex items-start gap-2.5">
          <div
            class="flex size-7 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary"
          >
            <IconifyIcon icon="lucide:info" class="size-4" />
          </div>
          <div class="flex-1 space-y-1">
            <div class="font-semibold text-foreground">
              {{ $t('ai.algorithm.uploadSpecTitle') }}
            </div>
            <p class="leading-relaxed">
              {{ $t('ai.algorithm.uploadSpecDesc') }}
            </p>
            <div class="flex flex-wrap gap-1.5 pt-1">
              <Tag class="font-mono text-[11px]">manifest.json</Tag>
              <Tag class="font-mono text-[11px]">config.schema.json</Tag>
              <Tag class="font-mono text-[11px]">testimage.jpg</Tag>
              <Tag class="font-mono text-[11px]">lib*.dylib / lib*.so</Tag>
            </div>
          </div>
        </div>
      </div>

      <!-- 上传拖拽区域 -->
      <div v-if="!uploading" class="relative">
        <Upload.Dragger
          name="file"
          :multiple="false"
          :before-upload="beforeUpload"
          :show-upload-list="false"
          accept=".zip,.tar.gz,.tgz,.tar"
          class="!border-dashed !border-2 !border-border hover:!border-primary rounded-xl !bg-card transition-colors p-8"
        >
          <div class="flex flex-col items-center justify-center space-y-3 py-4">
            <div
              class="flex size-14 items-center justify-center rounded-2xl bg-primary/10 text-primary shadow-xs"
            >
              <IconifyIcon icon="lucide:upload-cloud" class="size-7" />
            </div>
            <div class="text-center space-y-1">
              <p class="text-sm font-semibold text-foreground">
                {{ $t('ai.algorithm.uploadDragTitle') }}
              </p>
              <p class="text-xs text-muted-foreground max-w-sm mx-auto">
                {{ $t('ai.algorithm.uploadDragHint') }}
              </p>
            </div>
            <div class="flex items-center gap-2 pt-2">
              <Tag color="blue" class="font-mono">.zip</Tag>
              <Tag color="cyan" class="font-mono">.tar.gz</Tag>
              <Tag color="geekblue" class="font-mono">.tar</Tag>
            </div>
          </div>
        </Upload.Dragger>
      </div>

      <!-- 上传与沙箱自测中状态 -->
      <div
        v-else
        class="rounded-xl border border-border bg-card p-6 text-center space-y-4 shadow-xs"
      >
        <div class="flex items-center justify-center gap-3">
          <Spin size="default" />
          <span class="font-semibold text-foreground text-sm">{{
            uploadStatusText
          }}</span>
        </div>

        <div class="text-xs text-muted-foreground flex items-center justify-center gap-2 font-mono">
          <span>{{ selectedFileName }}</span>
          <span v-if="selectedFileSize">({{ formatBytes(selectedFileSize) }})</span>
        </div>

        <div class="pt-2">
          <Steps
            size="small"
            :current="uploadStep"
            :items="[
              { title: $t('system.common.upload') },
              { title: $t('ai.algorithm.statusActive') },
              { title: $t('system.common.success') },
            ]"
          />
        </div>
      </div>

      <!-- 错误提示 -->
      <Alert
        v-if="errorMessage"
        :message="$t('ai.algorithm.installFailed')"
        :description="errorMessage"
        type="error"
        show-icon
        class="rounded-lg"
      />
    </div>
  </Modal>
</template>
