<script lang="ts" setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue';

import { $t } from '@vben/locales';
import { useAccessStore } from '@vben/stores';

import { Image as AntImage, Spin } from 'ant-design-vue';

const props = withDefaults(
  defineProps<{
    alt?: string;
    fit?: 'contain' | 'cover' | 'fill';
    height?: number;
    preview?: boolean;
    url?: string;
    width?: number;
  }>(),
  {
    alt: '',
    fit: 'cover',
    height: 48,
    preview: true,
    url: '',
    width: 96,
  },
);

const accessStore = useAccessStore();
const imageSrc = ref<string>('');
const loading = ref<boolean>(false);
let objectUrlToRevoke = '';

const authUrl = computed(() => {
  if (!props.url) return '';
  const token = accessStore.accessToken;
  const delimiter = props.url.includes('?') ? '&' : '?';
  return token
    ? `${props.url}${delimiter}token=${encodeURIComponent(token)}`
    : props.url;
});

function cleanupObjectUrl() {
  if (objectUrlToRevoke) {
    URL.revokeObjectURL(objectUrlToRevoke);
    objectUrlToRevoke = '';
  }
}

onBeforeUnmount(() => {
  cleanupObjectUrl();
});

watch(
  () => props.url,
  async (newUrl) => {
    cleanupObjectUrl();
    if (!newUrl) {
      imageSrc.value = '';
      return;
    }
    loading.value = true;
    try {
      const resp = await fetch(newUrl, {
        headers: {
          Authorization: `Bearer ${accessStore.accessToken || ''}`,
        },
      });
      if (!resp.ok) {
        throw new Error(`HTTP ${resp.status}`);
      }
      const blob = await resp.blob();
      const objectUrl = URL.createObjectURL(blob);
      objectUrlToRevoke = objectUrl;
      imageSrc.value = objectUrl;
    } catch {
      imageSrc.value = authUrl.value;
    } finally {
      loading.value = false;
    }
  },
  { immediate: true },
);
</script>

<template>
  <div
    class="flex items-center justify-center overflow-hidden rounded bg-neutral-100 dark:bg-neutral-800"
    :style="{ width: `${width}px`, height: `${height}px` }"
  >
    <Spin v-if="loading" size="small" />
    <AntImage
      v-else-if="imageSrc"
      :alt="alt || $t('record.capture.drawer.noImage')"
      :height="height"
      :preview="preview"
      :src="imageSrc"
      :style="{ objectFit: fit }"
      :width="width"
    />
    <div v-else class="text-xs text-neutral-400 select-none">
      {{ $t('record.capture.drawer.noImage') }}
    </div>
  </div>
</template>
