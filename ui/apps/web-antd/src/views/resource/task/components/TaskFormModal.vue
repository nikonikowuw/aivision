<script lang="ts" setup>
import { computed, ref, watch } from 'vue';

import { $t } from '@vben/locales';

import {
  Alert,
  Form,
  FormItem,
  Input,
  message,
  Modal,
  Select,
} from 'ant-design-vue';

import {
  createTaskApi,
  getAvailableCamerasApi,
  type TaskApi,
  updateTaskApi,
} from '#/api';

interface Props {
  open?: boolean;
  task?: null | TaskApi.TaskItem;
}

const props = withDefaults(defineProps<Props>(), {
  open: false,
  task: null,
});

const emit = defineEmits<{
  (e: 'update:open', val: boolean): void;
  (e: 'success'): void;
}>();

const visible = computed({
  get: () => props.open,
  set: (val) => emit('update:open', val),
});

const isEdit = computed(() => Boolean(props.task));
const title = computed(() =>
  isEdit.value ? $t('resource.task.edit') : $t('resource.task.add'),
);

const submitting = ref(false);
const loadingCameras = ref(false);
const availableCameras = ref<TaskApi.AvailableCameraItem[]>([]);

const selectedCameraId = ref('');
const taskName = ref('');

const cameraOptions = computed(() => {
  return availableCameras.value.map((c) => ({
    label: `${c.name} (${c.cameraId})`,
    value: c.cameraId,
  }));
});

async function loadAvailableCameras() {
  if (isEdit.value) return;
  loadingCameras.value = true;
  try {
    const list = await getAvailableCamerasApi();
    availableCameras.value = list || [];
    if (availableCameras.value.length > 0 && !selectedCameraId.value) {
      selectedCameraId.value = availableCameras.value[0]?.cameraId || '';
    }
  } catch (err: any) {
    message.error(err.message || $t('resource.task.loadCamerasFailed'));
  } finally {
    loadingCameras.value = false;
  }
}

watch(
  () => props.open,
  (isOpen) => {
    if (!isOpen) return;
    if (props.task) {
      selectedCameraId.value = props.task.cameraId;
      taskName.value = props.task.name;
    } else {
      selectedCameraId.value = '';
      taskName.value = '';
      loadAvailableCameras();
    }
  },
);

async function handleOk() {
  if (!isEdit.value && !selectedCameraId.value) {
    message.error($t('resource.task.cameraRequired'));
    return;
  }
  if (!taskName.value.trim()) {
    message.error($t('resource.task.nameRequired'));
    return;
  }

  submitting.value = true;
  try {
    if (isEdit.value && props.task) {
      await updateTaskApi(props.task.cameraId, {
        name: taskName.value.trim(),
      });
      message.success($t('system.common.success'));
    } else {
      await createTaskApi({
        cameraId: selectedCameraId.value,
        name: taskName.value.trim(),
      });
      message.success($t('system.common.success'));
    }
    emit('success');
    visible.value = false;
  } catch {
    // 拦截器统一报错
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <Modal
    v-model:open="visible"
    :title="title"
    :confirm-loading="submitting"
    width="520px"
    @ok="handleOk"
  >
    <Form layout="vertical" class="mt-4">
      <FormItem
        v-if="!isEdit"
        :label="$t('resource.task.camera')"
        required
        class="mb-3"
      >
        <div v-if="!loadingCameras && cameraOptions.length === 0" class="mb-2">
          <Alert
            :message="$t('resource.task.noAvailableCameras')"
            type="warning"
            show-icon
          />
        </div>
        <Select
          v-model:value="selectedCameraId"
          :options="cameraOptions"
          :loading="loadingCameras"
          :placeholder="$t('resource.task.cameraSelectPlaceholder')"
          class="w-full"
        />
      </FormItem>

      <FormItem v-else :label="$t('resource.task.camera')" class="mb-3">
        <Input :value="selectedCameraId" disabled class="w-full" />
      </FormItem>

      <FormItem :label="$t('resource.task.name')" required class="mb-2">
        <Input
          v-model:value="taskName"
          :maxlength="128"
          :placeholder="$t('resource.task.namePlaceholder')"
          class="w-full"
        />
      </FormItem>
    </Form>
  </Modal>
</template>
