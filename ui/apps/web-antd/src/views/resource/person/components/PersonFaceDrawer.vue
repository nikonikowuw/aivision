<script lang="ts" setup>
import type { UploadProps } from 'ant-design-vue';

import type { PersonApi } from '#/api';

import { computed, ref, watch } from 'vue';

import { IconifyIcon } from '@vben/icons';
import { $t } from '@vben/locales';
import { useAccessStore } from '@vben/stores';

import {
  Alert,
  Button,
  Card,
  Drawer,
  Empty,
  message,
  Modal,
  Popconfirm,
  Radio,
  Space,
  Spin,
  Tag,
  Upload,
} from 'ant-design-vue';

import {
  deletePersonFaceApi,
  getPersonFaceImageUrl,
  listPersonFacesApi,
  registerPersonFaceApi,
} from '#/api';

import FaceThumbnail from './FaceThumbnail.vue';

interface Props {
  open: boolean;
  person: null | PersonApi.PersonItem;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  (e: 'update:open', val: boolean): void;
  (e: 'change'): void;
}>();

const accessStore = useAccessStore();

const visible = computed({
  get: () => props.open,
  set: (val) => emit('update:open', val),
});

const faces = ref<PersonApi.PersonFaceItem[]>([]);
const loading = ref<boolean>(false);
const uploading = ref<boolean>(false);

// 预览弹窗状态
const previewModalVisible = ref<boolean>(false);
const previewFace = ref<null | PersonApi.PersonFaceItem>(null);
const previewType = ref<'aligned' | 'raw'>('aligned');
const previewImageUrl = ref<string>('');
const previewImageLoading = ref<boolean>(false);

const isLimitReached = computed(() => faces.value.length >= 10);

watch(
  () => [props.open, props.person?.personId],
  async ([isOpen, personId]) => {
    if (isOpen && personId) {
      await fetchFaces();
    } else {
      faces.value = [];
    }
  },
);

async function fetchFaces() {
  if (!props.person) return;
  loading.value = true;
  try {
    const res = await listPersonFacesApi(props.person.personId);
    faces.value = res || [];
  } catch {
    // 错误由拦截器统一提示
  } finally {
    loading.value = false;
  }
}

const beforeUpload: UploadProps['beforeUpload'] = async (file) => {
  if (!props.person) return false;
  if (isLimitReached.value) {
    message.warning($t('resource.person.uploadFaceLimitTip'));
    return false;
  }

  const isAllowedType =
    file.type === 'image/jpeg' ||
    file.type === 'image/png' ||
    file.type === 'image/webp' ||
    file.name.endsWith('.jpg') ||
    file.name.endsWith('.jpeg') ||
    file.name.endsWith('.png') ||
    file.name.endsWith('.webp');

  if (!isAllowedType) {
    message.error($t('resource.person.uploadFaceTip'));
    return false;
  }

  const isLt10M = file.size / 1024 / 1024 <= 10;
  if (!isLt10M) {
    message.error($t('resource.person.uploadFaceTip'));
    return false;
  }

  uploading.value = true;
  try {
    await registerPersonFaceApi(props.person.personId, file);
    message.success($t('resource.person.uploadSuccess'));
    await fetchFaces();
    emit('change');
  } catch {
    // 错误由拦截器统一提示
  } finally {
    uploading.value = false;
  }
  return false;
};

async function handleDeleteFace(face: PersonApi.PersonFaceItem) {
  if (!props.person) return;
  try {
    await deletePersonFaceApi(props.person.personId, face.faceId);
    message.success($t('system.common.success'));
    await fetchFaces();
    emit('change');
  } catch {
    // 错误由拦截器统一提示
  }
}

async function loadAuthImageBlob(url: string): Promise<string> {
  const resp = await fetch(url, {
    headers: {
      Authorization: `Bearer ${accessStore.accessToken || ''}`,
    },
  });
  if (!resp.ok) {
    throw new Error(`HTTP ${resp.status}`);
  }
  const blob = await resp.blob();
  return URL.createObjectURL(blob);
}

async function openPreview(
  face: PersonApi.PersonFaceItem,
  type: 'aligned' | 'raw' = 'aligned',
) {
  if (!props.person) return;
  previewFace.value = face;
  previewType.value = type;
  previewModalVisible.value = true;
  await updatePreviewUrl();
}

async function updatePreviewUrl() {
  if (!props.person || !previewFace.value) return;
  if (previewImageUrl.value) {
    URL.revokeObjectURL(previewImageUrl.value);
    previewImageUrl.value = '';
  }
  previewImageLoading.value = true;
  try {
    const url = getPersonFaceImageUrl(
      props.person.personId,
      previewFace.value.faceId,
      previewType.value,
    );
    previewImageUrl.value = await loadAuthImageBlob(url);
  } catch {
    message.error($t('system.common.failed'));
  } finally {
    previewImageLoading.value = false;
  }
}

function handlePreviewModalClose() {
  previewModalVisible.value = false;
  if (previewImageUrl.value) {
    URL.revokeObjectURL(previewImageUrl.value);
    previewImageUrl.value = '';
  }
  previewFace.value = null;
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${(bytes / k ** i).toFixed(1)} ${sizes[i]}`;
}
</script>

<template>
  <Drawer
    v-model:open="visible"
    :title="
      $t('resource.person.faceDrawerTitle', {
        name: person?.name || '',
        personId: person?.personId || '',
      })
    "
    width="680"
    destroy-on-close
  >
    <div class="flex flex-col gap-4">
      <!-- 样本计数与上限提示 -->
      <div
        class="flex items-center justify-between rounded-lg border border-neutral-200 bg-neutral-50 px-4 py-3 dark:border-neutral-800 dark:bg-neutral-900"
      >
        <div class="flex items-center gap-2">
          <span class="text-sm font-medium">{{ $t('resource.person.faceCount') }}：</span>
          <Tag :color="isLimitReached ? 'error' : 'processing'">
            {{ faces.length }} / 10
          </Tag>
        </div>
        <span class="text-xs text-neutral-500">
          {{ $t('resource.person.uploadFaceTip') }}
        </span>
      </div>

      <!-- 上传区域 -->
      <div v-if="!isLimitReached">
        <Upload.Dragger
          name="file"
          :multiple="false"
          :show-upload-list="false"
          :before-upload="beforeUpload"
          :disabled="uploading"
          accept=".jpg,.jpeg,.png,.webp"
        >
          <Spin :spinning="uploading">
            <div
              class="flex flex-col items-center justify-center p-4 text-center"
            >
              <IconifyIcon icon="lucide:upload" class="size-8 text-primary" />
              <p class="mt-2 text-sm font-medium">
                {{ $t('resource.person.uploadFace') }}
              </p>
              <p class="text-xs text-neutral-400">
                {{ $t('resource.person.uploadFaceTip') }}
              </p>
            </div>
          </Spin>
        </Upload.Dragger>
      </div>
      <Alert
        v-else
        type="warning"
        show-icon
        :message="$t('resource.person.uploadFaceLimitTip')"
      />

      <!-- 样本列表 -->
      <div class="mt-2">
        <div class="mb-2 text-sm font-medium">
          {{ $t('resource.person.faceList') }}
        </div>
        <Spin :spinning="loading">
          <div v-if="faces.length === 0" class="py-8">
            <Empty :description="$t('resource.person.noFaces')" />
          </div>
          <div v-else class="grid grid-cols-1 gap-3">
            <Card
              v-for="face in faces"
              :key="face.faceId"
              size="small"
              class="border-neutral-200 shadow-sm dark:border-neutral-800"
            >
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-4">
                  <!-- 缩略图预览按钮 -->
                  <div
                    class="group relative cursor-pointer"
                    @click="openPreview(face, 'aligned')"
                  >
                    <FaceThumbnail
                      :url="
                        getPersonFaceImageUrl(
                          person?.personId || '',
                          face.faceId,
                          'aligned',
                        )
                      "
                      :size="72"
                    />
                    <div
                      class="absolute inset-0 flex items-center justify-center rounded-md bg-black/40 opacity-0 transition-opacity group-hover:opacity-100"
                    >
                      <IconifyIcon
                        icon="lucide:eye"
                        class="size-5 text-white"
                      />
                    </div>
                  </div>

                  <!-- 样本元信息 -->
                  <div class="flex flex-col gap-1 text-xs">
                    <div class="flex items-center gap-2">
                      <span
                        class="font-medium text-neutral-700 dark:text-neutral-300"
                      >
                        {{ $t('resource.person.qualityScore') }}:
                      </span>
                      <Tag
                        :color="
                          face.qualityScore >= 70
                            ? 'success'
                            : face.qualityScore >= 50
                              ? 'processing'
                              : 'warning'
                        "
                      >
                        {{ face.qualityScore.toFixed(1) }}
                      </Tag>
                      <span class="text-neutral-400">|</span>
                      <span class="text-neutral-500">
                        {{ $t('resource.person.detectionScore') }}:
                        {{ (face.detectionScore * 100).toFixed(0) }}%
                      </span>
                    </div>
                    <div class="text-neutral-500">
                      {{ $t('resource.person.imageSize') }}:
                      {{ formatBytes(face.rawImageSize) }} ({{
                        face.rawImageMime
                      }})
                    </div>
                    <div class="text-neutral-400">
                      {{ $t('resource.person.createdAt') }}:
                      {{ face.createdAt }}
                    </div>
                  </div>
                </div>

                <!-- 操作按钮 -->
                <Space>
                  <Button
                    size="small"
                    type="link"
                    @click="openPreview(face, 'aligned')"
                  >
                    {{ $t('resource.person.previewAligned') }}
                  </Button>
                  <Button
                    size="small"
                    type="link"
                    @click="openPreview(face, 'raw')"
                  >
                    {{ $t('resource.person.previewRaw') }}
                  </Button>
                  <Popconfirm
                    :title="$t('resource.person.deleteFaceConfirm')"
                    :ok-text="$t('system.common.confirm')"
                    :cancel-text="$t('system.common.cancel')"
                    @confirm="handleDeleteFace(face)"
                  >
                    <Button size="small" danger type="link">
                      <template #icon>
                        <IconifyIcon icon="lucide:trash-2" class="size-4" />
                      </template>
                      {{ $t('system.common.delete') }}
                    </Button>
                  </Popconfirm>
                </Space>
              </div>
            </Card>
          </div>
        </Spin>
      </div>
    </div>

    <!-- 预览弹窗 -->
    <Modal
      v-model:open="previewModalVisible"
      :title="
        previewType === 'raw'
          ? $t('resource.person.previewRaw')
          : $t('resource.person.previewAligned')
      "
      :footer="null"
      width="560px"
      @cancel="handlePreviewModalClose"
    >
      <div class="flex flex-col items-center gap-4 py-2">
        <Radio.Group
          v-model:value="previewType"
          button-style="solid"
          size="small"
          @change="updatePreviewUrl"
        >
          <Radio.Button value="aligned">
            {{ $t('resource.person.previewAligned') }} (112x112)
          </Radio.Button>
          <Radio.Button value="raw">
            {{ $t('resource.person.previewRaw') }}
          </Radio.Button>
        </Radio.Group>

        <Spin :spinning="previewImageLoading">
          <div
            class="flex max-h-[480px] max-w-full items-center justify-center overflow-hidden rounded-lg bg-neutral-100 p-2 dark:bg-neutral-800"
          >
            <img
              v-if="previewImageUrl"
              :src="previewImageUrl"
              class="max-h-[440px] max-w-full object-contain"
              alt="preview"
            />
          </div>
        </Spin>
      </div>
    </Modal>
  </Drawer>
</template>
