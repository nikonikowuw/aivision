<script setup lang="ts">
import type { Recordable } from '@vben/types';

import type { VbenFormSchema } from '#/adapter/form';

import { computed, ref } from 'vue';

import { ProfilePasswordSetting, z } from '@vben/common-ui';
import { $t } from '@vben/locales';

import { message } from 'ant-design-vue';

import { changeCurrentPasswordApi } from '#/api';
import { useAuthStore } from '#/store';

const authStore = useAuthStore();

const submitting = ref(false);

const formSchema = computed((): VbenFormSchema[] => {
  return [
    {
      component: 'VbenInputPassword',
      componentProps: {
        placeholder: $t('page.profile.oldPasswordPlaceholder'),
      },
      fieldName: 'oldPassword',
      label: $t('page.profile.oldPassword'),
      rules: z
        .string({ error: $t('page.profile.oldPasswordPlaceholder') })
        .min(1, { message: $t('page.profile.oldPasswordPlaceholder') }),
    },
    {
      component: 'VbenInputPassword',
      componentProps: {
        passwordStrength: true,
        placeholder: $t('page.profile.newPasswordPlaceholder'),
      },
      fieldName: 'newPassword',
      label: $t('page.profile.newPassword'),
      rules: z
        .string({ error: $t('page.profile.newPasswordPlaceholder') })
        .min(6, { message: $t('page.profile.newPasswordPlaceholder') })
        .max(32, { message: $t('page.profile.newPasswordPlaceholder') }),
    },
    {
      component: 'VbenInputPassword',
      componentProps: {
        passwordStrength: true,
        placeholder: $t('page.profile.confirmPasswordPlaceholder'),
      },
      dependencies: {
        rules(values) {
          const { newPassword } = values;
          return z
            .string({ error: $t('page.profile.confirmPasswordPlaceholder') })
            .min(1, { message: $t('page.profile.confirmPasswordPlaceholder') })
            .refine((value) => value === newPassword, {
              message: $t('page.profile.passwordMismatch'),
            });
        },
        triggerFields: ['newPassword'],
      },
      fieldName: 'confirmPassword',
      label: $t('page.profile.confirmPassword'),
    },
  ];
});

async function handleSubmit(values: Recordable<any>) {
  if (submitting.value) {
    return;
  }
  submitting.value = true;
  try {
    await changeCurrentPasswordApi({
      newPassword: values.newPassword,
      oldPassword: values.oldPassword,
    });
    message.success($t('page.profile.changePasswordSuccess'));
    await authStore.logout(true);
  } finally {
    submitting.value = false;
  }
}
</script>
<template>
  <ProfilePasswordSetting
    class="w-1/3"
    :form-schema="formSchema"
    @submit="handleSubmit"
  />
</template>
