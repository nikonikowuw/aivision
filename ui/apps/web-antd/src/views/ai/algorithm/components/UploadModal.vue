<script lang="ts" setup>
import type { UploadProps } from 'ant-design-vue';

import { computed, ref } from 'vue';

import { $t } from '@vben/locales';

import { Alert, message, Modal, Spin, Upload } from 'ant-design-vue';

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
const uploadStatusText = ref('');
const errorMessage = ref('');

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

  handleUpload(file);
  return false; // 阻止 Antd 默认自动上传
};

async function handleUpload(file: File) {
  uploading.value = true;
  errorMessage.value = '';
  uploadStatusText.value = $t('ai.algorithm.uploadingTip');

  try {
    uploadStatusText.value = $t('ai.algorithm.validatingTip');
    await uploadAlgorithmPackage(file);
    message.success($t('ai.algorithm.installSuccess'));
    emit('success');
    visible.value = false;
  } catch (error: any) {
    errorMessage.value = error?.message || $t('ai.algorithm.installFailed');
  } finally {
    uploading.value = false;
  }
}
</script>

<template>
  <Modal
    v-model:open="visible"
    :title="$t('ai.algorithm.uploadTitle')"
    :footer="null"
    width="600px"
    destroy-on-close
  >
    <div class="py-4">
      <Alert
        :message="$t('ai.algorithm.uploadTitle')"
        :description="$t('ai.algorithm.uploadHint')"
        type="info"
        show-icon
        class="mb-4"
      />

      <Spin :spinning="uploading" :tip="uploadStatusText">
        <Upload.Dragger
          name="file"
          :multiple="false"
          :before-upload="beforeUpload"
          :show-upload-list="false"
          accept=".zip,.tar.gz,.tgz,.tar"
          class="block py-6"
        >
          <p class="ant-upload-drag-icon text-4xl text-blue-500">
            <span class="anticon">📦</span>
          </p>
          <p class="ant-upload-text text-base font-semibold">
            {{ $t('ai.algorithm.uploadDragTitle') }}
          </p>
          <p class="ant-upload-hint text-gray-400">
            {{ $t('ai.algorithm.uploadDragHint') }}
          </p>
        </Upload.Dragger>
      </Spin>

      <Alert
        v-if="errorMessage"
        :message="$t('ai.algorithm.installFailed')"
        :description="errorMessage"
        type="error"
        show-icon
        class="mt-4"
      />
    </div>
  </Modal>
</template>
