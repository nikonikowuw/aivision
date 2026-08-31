<script lang="ts" setup>
import { computed } from 'vue';

const props = defineProps<{
  text?: string;
  color?: string;
  type?: string;
}>();

const tagStyle = computed(() => {
  const c = props.color?.toLowerCase() || 'blue';
  switch (c) {
    case 'yellow': {
      return {
        backgroundColor: '#fadb14',
        borderColor: '#d48806',
        color: '#1f1f1f',
        fontWeight: 'bold',
        letterSpacing: '1px',
      };
    }
    case 'green': {
      return {
        background: 'linear-gradient(180deg, #f6ffed 0%, #52c41a 100%)',
        borderColor: '#389e0d',
        color: '#135200',
        fontWeight: 'bold',
        letterSpacing: '1px',
      };
    }
    case 'white': {
      return {
        backgroundColor: '#ffffff',
        borderColor: '#d9d9d9',
        color: '#cf1322',
        fontWeight: 'bold',
        letterSpacing: '1px',
      };
    }
    case 'black': {
      return {
        backgroundColor: '#141414',
        borderColor: '#434343',
        color: '#ffffff',
        fontWeight: 'bold',
        letterSpacing: '1px',
      };
    }
    case 'blue':
    default: {
      return {
        backgroundColor: '#1677ff',
        borderColor: '#0958d9',
        color: '#ffffff',
        fontWeight: 'bold',
        letterSpacing: '1px',
      };
    }
  }
});
</script>

<template>
  <span
    class="inline-block rounded px-2 py-0.5 text-center font-mono text-sm shadow-sm transition-all"
    :style="tagStyle"
  >
    {{ text || '-' }}
  </span>
</template>
