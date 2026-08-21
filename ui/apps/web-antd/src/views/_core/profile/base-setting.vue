<script setup lang="ts">
import type { Recordable } from '@vben/types';

import type { VbenFormSchema } from '#/adapter/form';
import type { UserApi } from '#/api';

import { computed, onMounted, ref } from 'vue';

import { ProfileBaseSetting } from '@vben/common-ui';
import { $t } from '@vben/locales';

import { Avatar, Button, message, Upload } from 'ant-design-vue';

import {
  getCurrentProfileApi,
  updateCurrentProfileApi,
  uploadFileApi,
} from '#/api';
import { useAuthStore } from '#/store';

const authStore = useAuthStore();

/** 与后端 file-storage 上传约束对齐：仅图片格式，10 MiB */
const allowedAvatarTypes = new Set([
  'image/gif',
  'image/jpeg',
  'image/png',
  'image/webp',
]);
const maxAvatarSize = 10 * 1024 * 1024;

const profileBaseSettingRef = ref();
const submitting = ref(false);
const uploading = ref(false);

/** 头像区展示状态，随资料加载/保存统一更新 */
const displayProfile = ref<Pick<UserApi.CurrentProfile, 'avatar' | 'nickname'>>(
  {
    avatar: '',
    nickname: '',
  },
);

function applyDisplayProfile(
  data: Pick<UserApi.CurrentProfile, 'avatar' | 'nickname'>,
) {
  displayProfile.value = { avatar: data.avatar, nickname: data.nickname };
}

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

/**
 * 手动上传头像：拦截 antd 自动上传，改走文件上传服务换取 URL。
 * before-upload 返回 false 阻止默认上传行为。
 * 上传成功仅暂存 URL，需保存资料表单后才持久化。
 */
async function handleBeforeUpload(file: File) {
  if (!allowedAvatarTypes.has(file.type)) {
    message.error($t('page.profile.avatarInvalidType'));
    return false;
  }
  if (file.size > maxAvatarSize) {
    message.error($t('page.profile.avatarTooLarge'));
    return false;
  }
  if (uploading.value) {
    return false;
  }
  uploading.value = true;
  try {
    const result = await uploadFileApi(file);
    applyDisplayProfile({ ...displayProfile.value, avatar: result.url });
    message.success($t('page.profile.avatarUploadSuccess'));
  } finally {
    uploading.value = false;
  }
  return false;
}

async function handleSubmit(values: Recordable<any>) {
  if (submitting.value) {
    return;
  }
  submitting.value = true;
  try {
    const updated = await updateCurrentProfileApi({
      avatar: displayProfile.value.avatar,
      email: values.email,
      nickname: values.nickname,
      phone: values.phone,
      remark: values.remark,
    });
    profileBaseSettingRef.value?.getFormApi()?.setValues(updated);
    applyDisplayProfile(updated);
    await authStore.fetchUserInfo();
    message.success($t('page.profile.updateProfileSuccess'));
  } finally {
    submitting.value = false;
  }
}

onMounted(async () => {
  const data = await getCurrentProfileApi();
  profileBaseSettingRef.value?.getFormApi()?.setValues(data);
  applyDisplayProfile(data);
});
</script>
<template>
  <div>
    <div class="mb-6 flex items-center gap-4">
      <Avatar :size="72" :src="displayProfile.avatar">
        {{ displayProfile.nickname?.charAt(0) }}
      </Avatar>
      <Upload
        :before-upload="handleBeforeUpload"
        :show-upload-list="false"
        accept=".gif,.jpeg,.jpg,.png,.webp"
      >
        <Button :loading="uploading">
          {{ $t('page.profile.changeAvatar') }}
        </Button>
      </Upload>
    </div>
    <ProfileBaseSetting
      ref="profileBaseSettingRef"
      :form-schema="formSchema"
      @submit="handleSubmit"
    />
  </div>
</template>
