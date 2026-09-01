<script lang="ts" setup>
import { computed, ref } from 'vue';

import {
  Check,
  ChevronDown,
  ChevronRight,
  Eraser,
  Search,
  Settings,
} from '@vben/icons';
import { $t } from '@vben/locales';

import { Button, Checkbox, Input, Modal, Tag } from 'ant-design-vue';

interface CategoryGroup {
  classes: string[];
  defaultCollapsed?: boolean;
  icon: string;
  key: string;
  titleKey: string;
}

const ALL_GROUPS: CategoryGroup[] = [
  {
    key: 'person',
    titleKey: 'ai.classes.groups.person',
    icon: '🚶',
    classes: ['person'],
  },
  {
    key: 'vehicle',
    titleKey: 'ai.classes.groups.vehicle',
    icon: '🚗',
    classes: [
      'car',
      'truck',
      'bus',
      'motorcycle',
      'bicycle',
      'airplane',
      'train',
      'boat',
      'traffic light',
      'fire hydrant',
      'stop sign',
      'parking meter',
    ],
  },
  {
    key: 'animal',
    titleKey: 'ai.classes.groups.animal',
    icon: '🐾',
    classes: [
      'dog',
      'cat',
      'bird',
      'horse',
      'sheep',
      'cow',
      'elephant',
      'bear',
      'zebra',
      'giraffe',
    ],
  },
  {
    key: 'accessory',
    titleKey: 'ai.classes.groups.accessory',
    icon: '🎒',
    classes: ['backpack', 'umbrella', 'handbag', 'tie', 'suitcase'],
  },
  {
    key: 'indoor',
    titleKey: 'ai.classes.groups.indoor',
    icon: '🛋️',
    defaultCollapsed: true,
    classes: [
      'chair',
      'couch',
      'potted plant',
      'bed',
      'dining table',
      'toilet',
      'tv',
      'laptop',
      'mouse',
      'remote',
      'keyboard',
      'cell phone',
      'microwave',
      'oven',
      'toaster',
      'sink',
      'refrigerator',
      'book',
      'clock',
      'vase',
      'scissors',
      'teddy bear',
      'hair drier',
      'toothbrush',
    ],
  },
  {
    key: 'sports_food',
    titleKey: 'ai.classes.groups.sports_food',
    icon: '⚽',
    defaultCollapsed: true,
    classes: [
      'frisbee',
      'skis',
      'snowboard',
      'sports ball',
      'kite',
      'baseball bat',
      'baseball glove',
      'skateboard',
      'surfboard',
      'tennis racket',
      'bottle',
      'wine glass',
      'cup',
      'fork',
      'knife',
      'spoon',
      'bowl',
      'banana',
      'apple',
      'sandwich',
      'orange',
      'broccoli',
      'carrot',
      'hot dog',
      'pizza',
      'donut',
      'cake',
      'bench',
    ],
  },
];

const ALL_COCO_CLASSES = ALL_GROUPS.flatMap((g) => g.classes);

const PRESETS = [
  {
    id: 'common',
    labelKey: 'ai.classes.presets.common',
    icon: '👥',
    classes: ['person', 'car', 'motorcycle', 'bicycle', 'bus', 'truck'],
  },
  {
    id: 'personOnly',
    labelKey: 'ai.classes.presets.personOnly',
    icon: '🚶',
    classes: ['person'],
  },
  {
    id: 'vehicles',
    labelKey: 'ai.classes.presets.vehicles',
    icon: '🚗',
    classes: [
      'car',
      'truck',
      'bus',
      'motorcycle',
      'bicycle',
      'airplane',
      'train',
      'boat',
    ],
  },
  {
    id: 'pets',
    labelKey: 'ai.classes.presets.pets',
    icon: '🐕',
    classes: ['dog', 'cat'],
  },
  {
    id: 'accessories',
    labelKey: 'ai.classes.presets.accessories',
    icon: '📦',
    classes: ['backpack', 'handbag', 'suitcase'],
  },
];

const selectedValues = defineModel<string[]>('value', {
  default: () => ['person', 'car', 'motorcycle', 'bicycle', 'bus', 'truck'],
});

const modalVisible = ref(false);
const searchQuery = ref('');
const collapsedMap = ref<Record<string, boolean>>({
  indoor: true,
  sports_food: true,
});

function toggleCollapse(groupKey: string) {
  collapsedMap.value[groupKey] = !collapsedMap.value[groupKey];
}

function getClassTitle(cls: string): string {
  const i18nKey = `ai.classes.${cls}`;
  const translated = $t(i18nKey);
  return translated && translated !== i18nKey ? translated : cls;
}

function isSelected(cls: string): boolean {
  return selectedValues.value.includes(cls);
}

function toggleClass(cls: string) {
  const current = new Set(selectedValues.value);
  if (current.has(cls)) {
    current.delete(cls);
  } else {
    current.add(cls);
  }
  selectedValues.value = [...current];
}

function applyPreset(presetClasses: string[]) {
  selectedValues.value = [...presetClasses];
}

function isPresetActive(presetClasses: string[]): boolean {
  if (selectedValues.value.length !== presetClasses.length) return false;
  const set = new Set(selectedValues.value);
  return presetClasses.every((cls) => set.has(cls));
}

function isGroupAllSelected(group: CategoryGroup): boolean {
  return group.classes.every((cls) => selectedValues.value.includes(cls));
}

function getGroupSelectedCount(group: CategoryGroup): number {
  return group.classes.filter((cls) => selectedValues.value.includes(cls))
    .length;
}

function isGroupPartiallySelected(group: CategoryGroup): boolean {
  const count = getGroupSelectedCount(group);
  return count > 0 && count < group.classes.length;
}

function toggleGroup(group: CategoryGroup) {
  const allSelected = isGroupAllSelected(group);
  const current = new Set(selectedValues.value);
  if (allSelected) {
    for (const cls of group.classes) {
      current.delete(cls);
    }
  } else {
    for (const cls of group.classes) {
      current.add(cls);
    }
  }
  selectedValues.value = [...current];
}

const filteredGroups = computed(() => {
  const query = searchQuery.value.trim().toLowerCase();
  if (!query) return ALL_GROUPS;

  return ALL_GROUPS.map((group) => {
    const matchedClasses = group.classes.filter((cls) => {
      const label = getClassTitle(cls).toLowerCase();
      return cls.toLowerCase().includes(query) || label.includes(query);
    });
    return {
      ...group,
      classes: matchedClasses,
    };
  }).filter((group) => group.classes.length > 0);
});

const totalSelectedCount = computed(() => selectedValues.value.length);

// 当前活跃的预设名称（若吻合）
const currentMatchedPreset = computed(() => {
  return PRESETS.find((p) => isPresetActive(p.classes));
});
</script>

<template>
  <div class="categorized-class-summary-wrapper space-y-2.5">
    <!-- 1. 精简胶囊栏：展示已选目标 & 场景快捷切换 & 展开大面板按钮 -->
    <div
      class="flex flex-wrap items-center justify-between gap-2.5 rounded-xl border border-border/80 bg-muted/20 p-3"
    >
      <!-- 左侧：快捷预设或已选摘要 -->
      <div class="flex flex-wrap items-center gap-1.5 overflow-hidden">
        <!-- 预设标签（若匹配） -->
        <Tag
          v-if="currentMatchedPreset"
          color="blue"
          class="m-0 text-xs font-medium px-2 py-0.5"
        >
          {{ currentMatchedPreset.icon }}
          {{ $t(currentMatchedPreset.labelKey) }}
        </Tag>
        <span
          class="rounded-full bg-primary/10 text-primary border border-primary/20 px-2 py-0.5 text-xs font-mono font-semibold shrink-0"
        >
          {{ totalSelectedCount }} / 80
        </span>

        <!-- 精选目标 Chip 预览 (最多显示前 5 个) -->
        <div class="flex flex-wrap items-center gap-1">
          <span
            v-for="cls in selectedValues.slice(0, 5)"
            :key="cls"
            class="inline-flex items-center rounded-md bg-background px-2 py-0.5 text-[11px] font-medium text-foreground border border-border shadow-2xs"
          >
            {{ getClassTitle(cls) }}
          </span>
          <span
            v-if="selectedValues.length > 5"
            class="text-[11px] text-muted-foreground font-mono"
          >
            +{{ selectedValues.length - 5 }}...
          </span>
        </div>
      </div>

      <!-- 右侧：快捷预设与唤起完整选择抽屉/弹窗 -->
      <div class="flex items-center gap-1.5 shrink-0">
        <!-- 场景快速切换单选按钮 -->
        <Button
          v-for="preset in PRESETS.slice(0, 3)"
          :key="preset.id"
          size="small"
          :type="isPresetActive(preset.classes) ? 'primary' : 'default'"
          class="text-xs"
          @click="applyPreset(preset.classes)"
        >
          {{ preset.icon }} {{ $t(preset.labelKey) }}
        </Button>

        <Button
          type="dashed"
          size="small"
          class="flex items-center gap-1 text-xs"
          @click="modalVisible = true"
        >
          <Settings class="size-3.5" />
          <span>{{ $t('resource.task.instance.adjustTargets') }}</span>
        </Button>
      </div>
    </div>

    <!-- 2. 独立完整目标选择弹窗/抽屉 (不挤占主视区) -->
    <Modal
      v-model:open="modalVisible"
      :title="$t('resource.task.instance.adjustTargets')"
      width="780px"
      :footer="null"
      destroy-on-close
    >
      <div class="categorized-class-modal-body space-y-3.5 pt-2">
        <!-- 顶部状态栏与场景预设 -->
        <div
          class="flex flex-wrap items-center justify-between gap-2.5 border-b border-border/60 pb-3"
        >
          <div class="flex items-center gap-2">
            <span class="text-xs font-semibold text-foreground">
              {{
                $t('ai.classes.presets.selectedCount', {
                  count: totalSelectedCount,
                })
              }}
            </span>
            <span
              class="rounded-full px-2 py-0.5 text-[11px] font-mono font-medium"
              :class="
                totalSelectedCount > 0
                  ? 'bg-primary/10 text-primary border border-primary/20'
                  : 'bg-muted text-muted-foreground'
              "
            >
              {{ totalSelectedCount }} / 80
            </span>
          </div>

          <div class="w-full sm:w-64">
            <Input
              v-model:value="searchQuery"
              allow-clear
              size="small"
              class="rounded-lg text-xs"
              :placeholder="$t('ai.classes.presets.searchPlaceholder')"
            >
              <template #prefix>
                <Search class="size-3.5 text-muted-foreground mr-0.5" />
              </template>
            </Input>
          </div>
        </div>

        <!-- 场景预设一键选择 -->
        <div class="flex flex-wrap items-center gap-1.5">
          <span
            class="text-[11px] font-medium text-muted-foreground shrink-0 mr-1"
          >
            场景预设:
          </span>

          <button
            v-for="preset in PRESETS"
            :key="preset.id"
            type="button"
            class="inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs transition-all cursor-pointer border"
            :class="
              isPresetActive(preset.classes)
                ? 'bg-primary text-primary-foreground border-primary shadow-2xs font-medium'
                : 'bg-muted/50 hover:bg-muted text-foreground border-border/60 hover:border-primary/40'
            "
            @click="applyPreset(preset.classes)"
          >
            <span>{{ preset.icon }}</span>
            <span>{{ $t(preset.labelKey) }}</span>
            <span
              class="text-[10px] opacity-75 font-mono"
              :class="
                isPresetActive(preset.classes)
                  ? 'text-primary-foreground'
                  : 'text-muted-foreground'
              "
            >
              ({{ preset.classes.length }})
            </span>
          </button>

          <button
            type="button"
            class="inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs transition-all cursor-pointer border"
            :class="
              totalSelectedCount === ALL_COCO_CLASSES.length
                ? 'bg-primary text-primary-foreground border-primary shadow-2xs font-medium'
                : 'bg-muted/50 hover:bg-muted text-foreground border-border/60 hover:border-primary/40'
            "
            @click="applyPreset(ALL_COCO_CLASSES)"
          >
            <span>{{ $t('ai.classes.presets.all') }}</span>
            <span class="text-[10px] opacity-75 font-mono">(80)</span>
          </button>

          <button
            type="button"
            class="inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs transition-all cursor-pointer border border-transparent"
            :class="
              totalSelectedCount > 0
                ? 'text-red-500 hover:bg-red-50 dark:hover:bg-red-950/30'
                : 'text-muted-foreground/40 cursor-not-allowed'
            "
            :disabled="totalSelectedCount === 0"
            @click="applyPreset([])"
          >
            <Eraser class="size-3.5" />
            <span>{{ $t('ai.classes.presets.clear') }}</span>
          </button>
        </div>

        <!-- 80 类分组点选列表 (流畅滚动) -->
        <div
          class="max-h-[420px] space-y-2.5 overflow-y-auto pr-1 scrollbar-thin"
        >
          <div
            v-for="group in filteredGroups"
            :key="group.key"
            class="group-panel rounded-xl border border-border/70 bg-card/50 p-2.5 transition-all hover:border-border"
          >
            <div class="flex items-center justify-between pb-1.5">
              <div
                class="flex items-center gap-2 cursor-pointer select-none"
                @click="toggleCollapse(group.key)"
              >
                <component
                  :is="
                    collapsedMap[group.key] && !searchQuery
                      ? ChevronRight
                      : ChevronDown
                  "
                  class="size-3.5 text-muted-foreground transition-transform"
                />
                <span
                  class="text-sm font-medium text-foreground flex items-center gap-1.5"
                >
                  <span>{{ group.icon }}</span>
                  <span>{{ $t(group.titleKey) }}</span>
                </span>
                <span
                  v-if="getGroupSelectedCount(group) > 0"
                  class="rounded-full bg-primary/10 text-primary border border-primary/20 px-1.5 py-0.2 text-[10px] font-mono font-medium"
                >
                  {{ getGroupSelectedCount(group) }} /
                  {{ group.classes.length }}
                </span>
                <span
                  v-else
                  class="text-[11px] text-muted-foreground font-mono"
                >
                  ({{ group.classes.length }})
                </span>
              </div>

              <Checkbox
                :checked="isGroupAllSelected(group)"
                :indeterminate="isGroupPartiallySelected(group)"
                class="text-xs"
                @change="toggleGroup(group)"
              >
                <span
                  class="text-xs text-muted-foreground hover:text-foreground"
                >
                  {{
                    isGroupAllSelected(group)
                      ? $t('ai.classes.presets.unselectAllGroup')
                      : $t('ai.classes.presets.selectAllGroup')
                  }}
                </span>
              </Checkbox>
            </div>

            <div
              v-if="!collapsedMap[group.key] || searchQuery"
              class="flex flex-wrap gap-1.5 pt-1.5"
            >
              <button
                v-for="cls in group.classes"
                :key="cls"
                type="button"
                class="chip-button group/chip inline-flex items-center gap-1 rounded-lg px-2.5 py-1 text-xs transition-all cursor-pointer select-none border active:scale-95"
                :class="
                  isSelected(cls)
                    ? 'bg-primary text-primary-foreground border-primary shadow-2xs font-medium'
                    : 'bg-muted/40 hover:bg-muted/80 text-foreground border-border/70 hover:border-primary/50'
                "
                @click="toggleClass(cls)"
              >
                <Check
                  v-if="isSelected(cls)"
                  class="size-3 text-primary-foreground shrink-0 stroke-[2.5]"
                />
                <span class="leading-tight">{{ getClassTitle(cls) }}</span>
                <span
                  class="text-[10px] font-mono leading-none"
                  :class="
                    isSelected(cls)
                      ? 'text-primary-foreground/75'
                      : 'text-muted-foreground/70'
                  "
                >
                  {{ cls }}
                </span>
              </button>
            </div>
          </div>

          <div
            v-if="filteredGroups.length === 0"
            class="py-8 text-center text-xs text-muted-foreground"
          >
            <p>{{ $t('ai.classes.presets.emptySearch') }}</p>
          </div>
        </div>

        <!-- 弹窗底部确定按钮 -->
        <div class="flex justify-end pt-2 border-t">
          <Button type="primary" @click="modalVisible = false">
            {{ $t('system.common.confirm') }}
          </Button>
        </div>
      </div>
    </Modal>
  </div>
</template>

<style scoped>
.scrollbar-thin {
  scrollbar-color: var(--ant-color-border, rgb(0 0 0 / 15%)) transparent;
  scrollbar-width: thin;
}

.scrollbar-thin::-webkit-scrollbar {
  width: 6px;
}

.scrollbar-thin::-webkit-scrollbar-thumb {
  background-color: var(--ant-color-border, rgb(0 0 0 / 15%));
  border-radius: 9999px;
}
</style>
