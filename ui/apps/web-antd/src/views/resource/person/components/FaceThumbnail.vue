<script lang="ts" setup>
import { onBeforeUnmount, ref, watch } from 'vue';

import { IconifyIcon } from '@vben/icons';
import { useAccessStore } from '@vben/stores';

import { Spin } from 'ant-design-vue';

interface Props {
  url: string;
  alt?: string;
  size?: number;
}

const props = withDefaults(defineProps<Props>(), {
  alt: 'face',
  size: 64,
});

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
      imageSrc.value = '';
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
    class="flex items-center justify-center overflow-hidden rounded-md border border-neutral-200 bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-800"
    :style="{ width: `${size}px`, height: `${size}px` }"
  >
    <Spin v-if="loading" size="small" />
    <img
      v-else-if="imageSrc && !isError"
      :src="imageSrc"
      :alt="alt"
      class="h-full w-full object-cover"
    />
    <div
      v-else
      class="flex flex-col items-center justify-center text-neutral-400"
    >
      <IconifyIcon icon="lucide:user" class="size-6" />
    </div>
  </div>
</template>
