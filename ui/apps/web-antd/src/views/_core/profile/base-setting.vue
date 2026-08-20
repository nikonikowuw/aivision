<script setup lang="ts">
import type { Recordable } from '@vben/types';

import type { VbenFormSchema } from '#/adapter/form';

import { computed, onMounted, ref } from 'vue';

import { ProfileBaseSetting } from '@vben/common-ui';
import { $t } from '@vben/locales';

import { message } from 'ant-design-vue';

import { getCurrentProfileApi, updateCurrentProfileApi } from '#/api';
import { useAuthStore } from '#/store';

const authStore = useAuthStore();

const profileBaseSettingRef = ref();
const submitting = ref(false);

const formSchema = computed((): VbenFormSchema[] => {
  return [
    {
      component: 'Input',
      componentProps: {
        disabled: true,
      },
      fieldName: 'username',
      label: $t('page.profile.username'),
    },
    {
      component: 'Input',
      fieldName: 'nickname',
      label: $t('page.profile.nickname'),
    },
    {
      component: 'Input',
      fieldName: 'email',
      label: $t('page.profile.email'),
    },
    {
      component: 'Input',
      fieldName: 'phone',
      label: $t('page.profile.phone'),
    },
    {
      component: 'Textarea',
      fieldName: 'remark',
      label: $t('page.profile.remark'),
    },
  ];
});

async function handleSubmit(values: Recordable<any>) {
  if (submitting.value) {
    return;
  }
  submitting.value = true;
  try {
    const updated = await updateCurrentProfileApi({
      email: values.email,
      nickname: values.nickname,
      phone: values.phone,
      remark: values.remark,
    });
    profileBaseSettingRef.value?.getFormApi()?.setValues(updated);
    await authStore.fetchUserInfo();
    message.success($t('page.profile.updateProfileSuccess'));
  } finally {
    submitting.value = false;
  }
}

onMounted(async () => {
  const data = await getCurrentProfileApi();
  profileBaseSettingRef.value?.getFormApi()?.setValues(data);
});
</script>
<template>
  <ProfileBaseSetting
    ref="profileBaseSettingRef"
    :form-schema="formSchema"
    @submit="handleSubmit"
  />
</template>
