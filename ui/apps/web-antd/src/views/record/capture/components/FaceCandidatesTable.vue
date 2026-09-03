<script lang="ts" setup>
import { computed } from 'vue';

import { IconifyIcon } from '@vben/icons';
import { $t } from '@vben/locales';

import { Empty, Progress, Tag, Tooltip } from 'ant-design-vue';

import { getPersonFaceImageUrl } from '#/api';

import FaceThumbnail from '../../../resource/person/components/FaceThumbnail.vue';

export interface FaceCandidateItem {
  faceId?: string;
  face_id?: string;
  personId?: string;
  person_id?: string;
  personName?: string;
  person_name?: string;
  similarity: number;
}

const props = withDefaults(
  defineProps<{
    candidates?: FaceCandidateItem[];
    emptyText?: string;
    matchThreshold?: number;
    showMatchBadge?: boolean;
    top1Similarity?: number;
  }>(),
  {
    candidates: () => [],
    top1Similarity: undefined,
    showMatchBadge: false,
    matchThreshold: 0.7,
    emptyText: '',
  },
);

const effectiveCandidates = computed(() =>
  (props.candidates ?? []).map((cand) => ({
    faceId: cand.faceId || cand.face_id || '',
    personId: cand.personId || cand.person_id || '',
    personName: cand.personName || cand.person_name || '',
    similarity: cand.similarity,
  })),
);

function formatSimilarity(sim?: number): string {
  if (sim === undefined || sim === null || Number.isNaN(sim)) return '-';
  return `${(sim * 100).toFixed(1)}%`;
}

function getCandidateDiff(candSim: number): string {
  const top1 = props.top1Similarity ?? effectiveCandidates.value[0]?.similarity;
  if (top1 === undefined || top1 === null || Number.isNaN(top1)) return '-';
  const diff = top1 - candSim;
  if (diff <= 0.0001) return '0.0%';
  return `-${(diff * 100).toFixed(1)}%`;
}

function getSimilarityColor(sim: number): string {
  if (sim >= 0.8) return '#52c41a';
  if (sim >= 0.6) return '#1890ff';
  if (sim >= 0.4) return '#faad14';
  return '#8c8c8c';
}
</script>

<template>
  <div v-if="effectiveCandidates.length > 0" class="overflow-x-auto">
    <table class="candidate-table w-full text-xs text-center border-collapse">
      <thead>
        <tr
          class="border-b border-neutral-200 dark:border-neutral-700 text-neutral-500 bg-neutral-50/50 dark:bg-neutral-800/50 whitespace-nowrap"
        >
          <th class="py-2.5 px-3 w-14 text-center whitespace-nowrap">
            {{ $t('record.capture.drawer.candidatesRank') }}
          </th>
          <th class="py-2.5 px-3 w-16 text-center whitespace-nowrap">
            {{ $t('record.capture.drawer.candidatesPhoto') }}
          </th>
          <th class="py-2.5 px-3 min-w-[100px] text-center whitespace-nowrap">
            {{ $t('record.capture.drawer.candidatesPerson') }}
          </th>
          <th class="py-2.5 px-3 min-w-[120px] text-center whitespace-nowrap">
            {{ $t('record.capture.drawer.candidatesPersonId') }}
          </th>
          <th class="py-2.5 px-3 min-w-[180px] text-center whitespace-nowrap">
            {{ $t('record.capture.drawer.candidatesSimilarity') }}
          </th>
          <th class="py-2.5 px-3 min-w-[110px] text-center whitespace-nowrap">
            {{ $t('record.capture.drawer.candidatesDiff') }}
          </th>
          <th class="py-2.5 px-3 min-w-[140px] text-center whitespace-nowrap">
            {{ $t('record.capture.drawer.candidatesFaceId') }}
          </th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="(cand, idx) in effectiveCandidates"
          :key="cand.faceId || idx"
          class="border-b border-neutral-100 dark:border-neutral-800 transition-colors hover:bg-neutral-50 dark:hover:bg-neutral-800/40"
          :class="[
            idx === 0
              ? 'bg-primary/5 font-medium'
              : 'text-neutral-700 dark:text-neutral-300',
          ]"
        >
          <td class="py-2 px-3 text-center">
            <Tag
              :color="
                idx === 0
                  ? 'gold'
                  : idx === 1
                    ? 'blue'
                    : idx === 2
                      ? 'cyan'
                      : 'default'
              "
            >
              #{{ idx + 1 }}
            </Tag>
          </td>
          <td class="py-2 px-3 text-center">
            <FaceThumbnail
              v-if="cand.personId && cand.faceId"
              :url="
                getPersonFaceImageUrl(cand.personId, cand.faceId, 'aligned')
              "
              :size="44"
              :preview="true"
              :alt="cand.personName || 'Candidate'"
              class="mx-auto shadow-sm"
            />
            <div
              v-else
              class="w-11 h-11 mx-auto flex items-center justify-center rounded border border-neutral-200 bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-800 text-neutral-400"
            >
              <IconifyIcon icon="lucide:user" class="size-5" />
            </div>
          </td>
          <td class="py-2 px-3 text-center">
            <div class="flex items-center justify-center gap-1.5">
              <span :class="[idx === 0 ? 'font-semibold text-primary' : '']">
                {{ cand.personName || '-' }}
              </span>
              <Tag
                v-if="
                  idx === 0 &&
                  showMatchBadge &&
                  cand.similarity >= matchThreshold
                "
                color="green"
                class="m-0 scale-90"
              >
                {{ $t('record.capture.drawer.matched') }}
              </Tag>
            </div>
          </td>
          <td
            class="py-2 px-3 text-center font-mono text-neutral-600 dark:text-neutral-400"
          >
            <span v-if="cand.personId" class="inline-block whitespace-nowrap">
              {{ cand.personId }}
            </span>
            <span v-else>-</span>
          </td>
          <td class="py-2 px-3 text-center">
            <div
              class="mx-auto flex max-w-[160px] items-center justify-center gap-2"
            >
              <Progress
                :percent="Number((cand.similarity * 100).toFixed(1))"
                :stroke-color="getSimilarityColor(cand.similarity)"
                :show-info="false"
                size="small"
                class="flex-1 !m-0"
              />
              <span
                class="w-12 text-right font-mono font-semibold"
                :style="{ color: getSimilarityColor(cand.similarity) }"
              >
                {{ formatSimilarity(cand.similarity) }}
              </span>
            </div>
          </td>
          <td
            class="py-2 px-3 text-center font-mono text-neutral-500 dark:text-neutral-400"
          >
            {{ idx === 0 ? '0.0%' : getCandidateDiff(cand.similarity) }}
          </td>
          <td class="py-2 px-3 text-center font-mono text-neutral-400 text-xs">
            <Tooltip v-if="cand.faceId" :title="cand.faceId">
              <span class="inline-block max-w-[140px] truncate text-center">
                {{ cand.faceId }}
              </span>
            </Tooltip>
            <span v-else>-</span>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
  <Empty
    v-else
    :description="emptyText || $t('record.capture.drawer.noCandidates')"
    :image="Empty.PRESENTED_IMAGE_SIMPLE"
  />
</template>

<style scoped>
.candidate-table th,
.candidate-table td {
  vertical-align: middle !important;
  text-align: center !important;
}
</style>
