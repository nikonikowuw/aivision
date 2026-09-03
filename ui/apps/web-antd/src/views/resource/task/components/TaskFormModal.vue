<script lang="ts" setup>
import type { TaskApi } from '#/api';

import { computed, ref, watch } from 'vue';

import { IconifyIcon } from '@vben/icons';
import { $t } from '@vben/locales';

import {
  Alert,
  Form,
  FormItem,
  Input,
  message,
  Modal,
  Spin,
  Tag,
} from 'ant-design-vue';

import { createTaskApi, getAvailableCamerasApi, updateTaskApi } from '#/api';

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
const cameraKeyword = ref('');

const selectedCameraId = ref('');
const taskName = ref('');

const filteredCameras = computed(() => {
  const kw = cameraKeyword.value.trim().toLowerCase();
  if (!kw) return availableCameras.value;
  return availableCameras.value.filter(
    (c) =>
      c.name.toLowerCase().includes(kw) ||
      c.cameraId.toLowerCase().includes(kw),
  );
});

async function loadAvailableCameras() {
  if (isEdit.value) return;
  loadingCameras.value = true;
  cameraKeyword.value = '';
  try {
    const list = await getAvailableCamerasApi();
    availableCameras.value = list || [];
    if (availableCameras.value.length > 0 && !selectedCameraId.value) {
      selectCamera(availableCameras.value[0]?.cameraId || '');
    }
  } catch {
    message.error($t('resource.task.loadCamerasFailed'));
  } finally {
    loadingCameras.value = false;
  }
}

function selectCamera(id: string) {
  selectedCameraId.value = id;
  const cam = availableCameras.value.find((c) => c.cameraId === id);
  if (cam && (!taskName.value.trim() || taskName.value.endsWith('分析任务'))) {
    taskName.value = `${cam.name} 分析任务`;
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
    width="580px"
    destroy-on-close
    @ok="handleOk"
  >
    <Form layout="vertical" class="mt-3">
      <!-- 摄像头选择 (可视化视频源卡片列表) -->
      <FormItem
        v-if="!isEdit"
        :label="$t('resource.task.camera')"
        required
        class="mb-3"
      >
        <div
          v-if="!loadingCameras && availableCameras.length === 0"
          class="mb-2"
        >
          <Alert
            :message="$t('resource.task.noAvailableCameras')"
            type="warning"
            show-icon
          />
        </div>

        <div v-else class="space-y-2">
          <!-- 快捷检索过滤框 -->
          <div v-if="availableCameras.length > 3" class="relative">
            <Input
              v-model:value="cameraKeyword"
              placeholder="搜索摄像头名称或 ID..."
              size="small"
              allow-clear
            >
              <template #prefix>
                <IconifyIcon
                  icon="lucide:search"
                  class="text-muted-foreground size-3.5"
                />
              </template>
            </Input>
          </div>

          <!-- 可视化摄像头源卡片网格 -->
          <Spin :spinning="loadingCameras">
            <div class="max-h-[250px] overflow-y-auto pr-1 space-y-2">
              <div
                v-for="cam in filteredCameras"
                :key="cam.cameraId"
                @click="selectCamera(cam.cameraId)"
                class="cursor-pointer rounded-xl border p-3 transition-all duration-150 flex items-center justify-between group"
                :class="[
                  selectedCameraId === cam.cameraId
                    ? 'border-primary bg-primary/[0.04] ring-2 ring-primary/20 shadow-xs'
                    : 'border-border bg-card hover:border-primary/50',
                ]"
              >
                <div class="flex items-center gap-3 min-w-0">
                  <div
                    class="flex size-9 shrink-0 items-center justify-center rounded-lg transition-colors"
                    :class="[
                      selectedCameraId === cam.cameraId
                        ? 'bg-primary/10 text-primary'
                        : 'bg-muted text-muted-foreground group-hover:text-foreground',
                    ]"
                  >
                    <IconifyIcon icon="lucide:video" class="size-4" />
                  </div>
                  <div class="min-w-0">
                    <div class="flex items-center gap-1.5 flex-wrap">
                      <span
                        class="text-xs font-semibold text-foreground truncate max-w-[140px] sm:max-w-none"
                      >
                        {{ cam.name }}
                      </span>
                      <span
                        class="text-[10px] font-mono px-1 rounded bg-muted text-muted-foreground shrink-0"
                      >
                        {{ cam.cameraId }}
                      </span>
                    </div>
                    <div
                      class="flex flex-wrap items-center gap-1.5 mt-1 text-[10px] text-muted-foreground font-mono"
                    >
                      <Tag color="blue" class="m-0 text-[9px] px-1 py-0">
                        RTSP
                      </Tag>
                      <span>1080P@25fps</span>
                      <span>·</span>
                      <span>硬件解码</span>
                    </div>
                  </div>
                </div>

                <div class="shrink-0 pl-2">
                  <div
                    v-if="selectedCameraId === cam.cameraId"
                    class="flex size-5 rounded-full bg-primary items-center justify-center text-primary-foreground shadow-xs"
                  >
                    <IconifyIcon icon="lucide:check" class="size-3" />
                  </div>
                  <div
                    v-else
                    class="size-5 rounded-full border border-border group-hover:border-primary/50"
                  ></div>
                </div>
              </div>

              <div
                v-if="filteredCameras.length === 0 && !loadingCameras"
                class="py-6 text-center text-xs text-muted-foreground"
              >
                未匹配到符合条件的摄像头
              </div>
            </div>
          </Spin>
        </div>
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
