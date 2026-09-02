<script lang="ts" setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue';

import { useAccessStore } from '@vben/stores';

import { Image as AntImage, Spin } from 'ant-design-vue';

const props = withDefaults(
  defineProps<{
    url?: string;
    width?: number;
    height?: number;
    alt?: string;
    fit?: 'contain' | 'cover' | 'fill';
  }>(),
  {
    fit: 'cover',
    height: 48,
    url: '',
    width: 96,
  },
);

const accessStore = useAccessStore();
const imageSrc = ref<string>('');
const loading = ref<boolean>(false);
const isError = ref<boolean>(false);

let currentBlobUrl = '';

function cleanup() {
  if (currentBlobUrl) {
    URL.revokeObjectURL(currentBlobUrl);
    currentBlobUrl = '';
  }
}

const authUrl = computed(() => {
  if (!props.url) return '';
  const token = accessStore.accessToken;
  const delimiter = props.url.includes('?') ? '&' : '?';
  return token ? `${props.url}${delimiter}token=${encodeURIComponent(token)}` : props.url;
});

watch(
  () => props.url,
  async (newUrl) => {
    cleanup();
    if (!newUrl) {
      imageSrc.value = '';
      return;
    }
    isError.value = false;
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
      currentBlobUrl = URL.createObjectURL(blob);
      imageSrc.value = currentBlobUrl;
    } catch {
      isError.value = true;
      imageSrc.value = authUrl.value;
    } finally {
      loading.value = false;
    }
  },
  { immediate: true },
);

onBeforeUnmount(() => {
  cleanup();
});
</script>

<template>
  <div
    class="flex mx-auto items-center justify-center overflow-hidden rounded bg-neutral-100 dark:bg-neutral-800"
    :style="{ width: `${width}px`, height: `${height}px` }"
  >
    <Spin v-if="loading" size="small" />
    <div
      v-else-if="!url || isError"
      class="text-xs text-neutral-400"
    >
      -
    </div>
    <AntImage
      v-else
      :alt="alt || 'plate'"
      class="rounded"
      :height="height"
      :src="imageSrc"
      :style="{ objectFit: fit }"
      :width="width"
    />
  </div>
</template>
