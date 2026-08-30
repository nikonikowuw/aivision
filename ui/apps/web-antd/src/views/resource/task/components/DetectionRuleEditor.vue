<script setup lang="ts">
import type { NormalizedPoint, VideoViewport } from './rule-geometry';

import type { TaskApi } from '#/api';
import type { CameraApi } from '#/api/core/camera';

import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from 'vue';

import { IconifyIcon } from '@vben/icons';
import { $t } from '@vben/locales';

import {
  Alert,
  Button,
  Empty,
  Popconfirm,
  Select,
  Tag,
  Tooltip,
} from 'ant-design-vue';

import {
  getCameraPageApi,
  startLivePreviewApi,
  stopLivePreviewApi,
} from '#/api';
import VideoPlayer from '#/components/video/VideoPlayer.vue';

import {
  denormalizePoint,
  hasSelfIntersection,
  normalizePoint,
} from './rule-geometry';

interface Props {
  cameraId: string;
  open?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  open: false,
});

const rules = defineModel<TaskApi.DetectionRule[]>('value', {
  default: () => [],
});

const ROLE = {
  LINE: 3,
  MASK: 2,
  ROI: 1,
} as const;
type RuleRole = (typeof ROLE)[keyof typeof ROLE];

const LINE_DIRECTION = {
  A_TO_B: 1,
  B_TO_A: 2,
  BOTH: 0,
} as const;

const STREAM_TYPE = 'main' as const;
const MIN_REGION_POINTS = 3;
const MIN_LINE_POINTS = 2;
const VERTEX_HIT_RADIUS = 12;
const POINT_EQUAL_EPSILON = 0.006;
const ARROW_LENGTH = 14;
const ARROW_WIDTH = 5;
const ARROW_POS_SINGLE = 0.5;
const ARROW_POS_FORWARD = 0.38;
const ARROW_POS_BACKWARD = 0.62;
const VERTEX_RADIUS_DEFAULT = 5;
const VERTEX_RADIUS_SELECTED = 7;
const MASK_LINE_DASH = [7, 5];
const DRAFT_LINE_DASH = [5, 4];
const CAMERA_PAGE_MAX_SIZE = 100;

interface RuleStyle {
  fill: string;
  lineDash: number[];
  stroke: string;
}

interface VertexTarget {
  draft: boolean;
  pointIndex: number;
  ruleIndex: number;
}

interface PointerState {
  local: NormalizedPoint;
  normalized: NormalizedPoint | null;
}

interface DraggingVertex extends VertexTarget {
  moved: boolean;
  pointerId: number;
}

type ValidationIssue =
  | { kind: 'invalid' }
  | { kind: 'lineDirection' }
  | { kind: 'outOfBounds' }
  | { kind: 'selfIntersect' }
  | { kind: 'tooFewPoints'; minimum: number };

const roleOptions: Array<{ icon: string; role: RuleRole }> = [
  { icon: 'lucide:scan-area', role: ROLE.ROI },
  { icon: 'lucide:ban', role: ROLE.MASK },
  { icon: 'lucide:move-right', role: ROLE.LINE },
];

const canvasRef = ref<HTMLCanvasElement | null>(null);
const canvasSize = ref({ height: 0, width: 0 });
const devicePixelRatio = ref(1);
const mediaWidth = ref(0);
const mediaHeight = ref(0);
const metadataReady = ref(false);

const draftRule = ref<null | TaskApi.DetectionRule>(null);
const selectedRuleIndex = ref<null | number>(null);
const selectedVertex = ref<null | VertexTarget>(null);
const draggingVertex = ref<DraggingVertex | null>(null);
const hoverPoint = ref<NormalizedPoint | null>(null);
const suppressNextClick = ref(false);

const streamUrl = ref('');
const streamLoading = ref(false);
const streamError = ref('');
const cameraNumericId = ref<null | number>(null);
let streamSession = 0;
let resizeObserver: null | ResizeObserver = null;

const canDraw = computed(
  () =>
    Boolean(streamUrl.value) &&
    mediaWidth.value > 0 &&
    mediaHeight.value > 0 &&
    metadataReady.value &&
    !streamError.value &&
    canvasSize.value.width > 0 &&
    canvasSize.value.height > 0,
);

const directionOptions = computed(() => [
  {
    label: $t('resource.task.ruleEditor.direction.both'),
    value: LINE_DIRECTION.BOTH,
  },
  {
    label: $t('resource.task.ruleEditor.direction.aToB'),
    value: LINE_DIRECTION.A_TO_B,
  },
  {
    label: $t('resource.task.ruleEditor.direction.bToA'),
    value: LINE_DIRECTION.B_TO_A,
  },
]);

const validationIssue = computed<null | ValidationIssue>(() => {
  if (draftRule.value) {
    return validateRule(draftRule.value);
  }
  for (const rule of rules.value) {
    const issue = validateRule(rule);
    if (issue) return issue;
  }
  return null;
});

const validationMessage = computed(() => {
  return formatValidationIssue(validationIssue.value);
});

const ruleCountLabel = computed(() =>
  $t('resource.task.ruleEditor.ruleCount', { count: rules.value.length }),
);

const canDeleteSelectedVertex = computed(() => {
  const target = selectedVertex.value;
  if (!target) return false;
  const rule = target.draft ? draftRule.value : rules.value[target.ruleIndex];
  return Boolean(rule && rule.points.length > getMinimumPoints(rule.role));
});

function cloneRule(rule: TaskApi.DetectionRule): TaskApi.DetectionRule {
  return {
    lineDirection: rule.lineDirection,
    points: rule.points.map((point) => ({ x: point.x, y: point.y })),
    role: rule.role,
  };
}

function cloneRules(source: readonly TaskApi.DetectionRule[]) {
  return source.map((rule) => cloneRule(rule));
}

function isAreaRole(role: number): boolean {
  return role === ROLE.ROI || role === ROLE.MASK;
}

function getMinimumPoints(role: number): number {
  return isAreaRole(role) ? MIN_REGION_POINTS : MIN_LINE_POINTS;
}

function getRoleLabel(role: number): string {
  switch (role) {
    case ROLE.LINE: {
      return $t('resource.task.ruleEditor.role.line');
    }
    case ROLE.MASK: {
      return $t('resource.task.ruleEditor.role.mask');
    }
    case ROLE.ROI: {
      return $t('resource.task.ruleEditor.role.roi');
    }
    default: {
      return $t('resource.task.ruleEditor.role.unknown');
    }
  }
}

function getDirectionLabel(direction: number): string {
  switch (direction) {
    case LINE_DIRECTION.A_TO_B: {
      return $t('resource.task.ruleEditor.direction.aToB');
    }
    case LINE_DIRECTION.B_TO_A: {
      return $t('resource.task.ruleEditor.direction.bToA');
    }
    default: {
      return $t('resource.task.ruleEditor.direction.both');
    }
  }
}

function getRuleStyle(role: number): RuleStyle {
  switch (role) {
    case ROLE.LINE: {
      return {
        fill: 'transparent',
        lineDash: [],
        stroke: '#fb7185',
      };
    }
    case ROLE.MASK: {
      return {
        fill: 'rgba(249, 115, 22, 0.2)',
        lineDash: MASK_LINE_DASH,
        stroke: '#fb923c',
      };
    }
    default: {
      return {
        fill: 'rgba(34, 211, 238, 0.18)',
        lineDash: [],
        stroke: '#22d3ee',
      };
    }
  }
}

function validateRule(rule: TaskApi.DetectionRule): null | ValidationIssue {
  if (
    rule.role !== ROLE.ROI &&
    rule.role !== ROLE.MASK &&
    rule.role !== ROLE.LINE
  ) {
    return { kind: 'invalid' };
  }

  const points = Array.isArray(rule.points) ? rule.points : [];
  const minimum = getMinimumPoints(rule.role);
  if (points.length < minimum) {
    return { kind: 'tooFewPoints', minimum };
  }

  if (!isAreaRole(rule.role) && rule.lineDirection !== LINE_DIRECTION.BOTH) {
    return { kind: 'lineDirection' };
  }

  if (
    points.some(
      (point) =>
        !Number.isFinite(point.x) ||
        !Number.isFinite(point.y) ||
        point.x < 0 ||
        point.x > 1 ||
        point.y < 0 ||
        point.y > 1,
    )
  ) {
    return { kind: 'outOfBounds' };
  }

  if (isAreaRole(rule.role) && hasSelfIntersection(points)) {
    return { kind: 'selfIntersect' };
  }

  return null;
}

function formatValidationIssue(issue: null | ValidationIssue): string {
  if (!issue) return '';
  switch (issue.kind) {
    case 'lineDirection': {
      return $t('resource.task.ruleEditor.validation.lineDirection');
    }
    case 'outOfBounds': {
      return $t('resource.task.ruleEditor.validation.outOfBounds');
    }
    case 'selfIntersect': {
      return $t('resource.task.ruleEditor.validation.selfIntersect');
    }
    case 'tooFewPoints': {
      return $t('resource.task.ruleEditor.validation.tooFewPoints', {
        count: issue.minimum,
      });
    }
    default: {
      return $t('resource.task.ruleEditor.validation.invalid');
    }
  }
}

function createRule(role: RuleRole): TaskApi.DetectionRule {
  return {
    lineDirection: LINE_DIRECTION.BOTH,
    points: [],
    role,
  };
}

function resetInteraction() {
  draftRule.value = null;
  selectedRuleIndex.value = null;
  selectedVertex.value = null;
  draggingVertex.value = null;
  hoverPoint.value = null;
  suppressNextClick.value = false;
}

function startDrawing(role: RuleRole) {
  if (!canDraw.value) return;
  if (draftRule.value && !finishDraft()) return;
  draftRule.value = createRule(role);
  selectedRuleIndex.value = null;
  selectedVertex.value = null;
  hoverPoint.value = null;
}

function cancelDrawing() {
  resetInteraction();
}

function finishDraft(): boolean {
  const draft = draftRule.value;
  if (!draft) return true;

  const issue = validateRule(draft);
  if (issue) {
    return false;
  }

  const nextRules = [...cloneRules(rules.value), cloneRule(draft)];
  rules.value = nextRules;
  selectedRuleIndex.value = nextRules.length - 1;
  selectedVertex.value = null;
  draftRule.value = null;
  hoverPoint.value = null;
  return true;
}

function addDraftPoint(point: NormalizedPoint) {
  const draft = draftRule.value;
  if (!draft) return;
  draftRule.value = {
    ...draft,
    points: [...draft.points, { x: point.x, y: point.y }],
  };
}

function updateRulePoint(target: VertexTarget, point: NormalizedPoint) {
  if (target.draft) {
    const draft = draftRule.value;
    if (!draft || !draft.points[target.pointIndex]) return;
    const points = draft.points.map((currentPoint, index) =>
      index === target.pointIndex ? { x: point.x, y: point.y } : currentPoint,
    );
    draftRule.value = { ...draft, points };
    return;
  }

  const currentRule = rules.value[target.ruleIndex];
  if (!currentRule || !currentRule.points[target.pointIndex]) return;
  const nextRules = cloneRules(rules.value);
  const nextRule = nextRules[target.ruleIndex];
  if (!nextRule) return;
  nextRule.points[target.pointIndex] = { x: point.x, y: point.y };
  rules.value = nextRules;
}

function updateLineDirection(ruleIndex: number, value: unknown) {
  if (
    value !== LINE_DIRECTION.BOTH &&
    value !== LINE_DIRECTION.A_TO_B &&
    value !== LINE_DIRECTION.B_TO_A
  ) {
    return;
  }
  const rule = rules.value[ruleIndex];
  if (!rule || rule.role !== ROLE.LINE) return;
  const nextRules = cloneRules(rules.value);
  const nextRule = nextRules[ruleIndex];
  if (!nextRule) return;
  nextRule.lineDirection = value;
  rules.value = nextRules;
}

function selectRule(ruleIndex: number) {
  selectedRuleIndex.value = ruleIndex;
  selectedVertex.value = null;
}

function deleteRule(ruleIndex: number) {
  if (!rules.value[ruleIndex]) return;
  rules.value = rules.value.filter((_rule, index) => index !== ruleIndex);
  selectedVertex.value = null;
  if (selectedRuleIndex.value === ruleIndex) {
    selectedRuleIndex.value = null;
  } else if (
    selectedRuleIndex.value !== null &&
    selectedRuleIndex.value > ruleIndex
  ) {
    selectedRuleIndex.value -= 1;
  }
}

function clearRules() {
  rules.value = [];
  resetInteraction();
}

function removeSelectedVertex() {
  const target = selectedVertex.value;
  if (!target) return;

  const rule = target.draft ? draftRule.value : rules.value[target.ruleIndex];
  if (!rule) return;
  const minimum = getMinimumPoints(rule.role);
  if (rule.points.length <= minimum) {
    return;
  }

  if (target.draft) {
    draftRule.value = {
      ...rule,
      points: rule.points.filter(
        (_point, index) => index !== target.pointIndex,
      ),
    };
  } else {
    const nextRules = cloneRules(rules.value);
    const nextRule = nextRules[target.ruleIndex];
    if (!nextRule) return;
    nextRule.points = nextRule.points.filter(
      (_point, index) => index !== target.pointIndex,
    );
    rules.value = nextRules;
  }
  selectedVertex.value = null;
}

function getCanvasMetrics() {
  const canvas = canvasRef.value;
  if (!canvas) return null;

  const rect = canvas.getBoundingClientRect();
  const width = canvas.offsetWidth || rect.width;
  const height = canvas.offsetHeight || rect.height;
  if (width <= 0 || height <= 0 || rect.width <= 0 || rect.height <= 0) {
    return null;
  }

  return { height, rect, width };
}

function getViewport(): VideoViewport {
  const metrics = getCanvasMetrics();
  return {
    elementHeight: metrics?.height ?? canvasSize.value.height,
    elementWidth: metrics?.width ?? canvasSize.value.width,
    videoHeight: mediaHeight.value,
    videoWidth: mediaWidth.value,
  };
}

function getPointerState(
  event: MouseEvent | PointerEvent,
  clampToViewport = false,
): null | PointerState {
  const canvas = canvasRef.value;
  if (!canvas || !canDraw.value) return null;

  const metrics = getCanvasMetrics();
  if (!metrics) return null;

  const scaleX = metrics.width / metrics.rect.width;
  const scaleY = metrics.height / metrics.rect.height;
  const local = {
    x: (event.clientX - metrics.rect.left) * scaleX,
    y: (event.clientY - metrics.rect.top) * scaleY,
  };
  return {
    local,
    normalized: normalizePoint(local, getViewport(), clampToViewport),
  };
}

function getCanvasPoint(point: NormalizedPoint): NormalizedPoint {
  return denormalizePoint(point, getViewport());
}

function getNearestVertex(local: NormalizedPoint): null | VertexTarget {
  let nearest: null | VertexTarget = null;
  let nearestDistance = VERTEX_HIT_RADIUS;

  const consider = (point: NormalizedPoint, target: VertexTarget): void => {
    const canvasPoint = getCanvasPoint(point);
    const distance = Math.hypot(
      canvasPoint.x - local.x,
      canvasPoint.y - local.y,
    );
    if (distance <= nearestDistance) {
      nearestDistance = distance;
      nearest = target;
    }
  };

  rules.value.forEach((rule, ruleIndex) => {
    rule.points.forEach((point, pointIndex) => {
      consider(point, { draft: false, pointIndex, ruleIndex });
    });
  });

  if (draftRule.value) {
    draftRule.value.points.forEach((point, pointIndex) => {
      consider(point, { draft: true, pointIndex, ruleIndex: -1 });
    });
  }

  return nearest;
}

function selectVertex(target: VertexTarget) {
  selectedVertex.value = target;
  selectedRuleIndex.value = target.draft ? null : target.ruleIndex;
}

function handlePointerDown(event: PointerEvent) {
  const state = getPointerState(event);
  if (!state) return;

  const target = getNearestVertex(state.local);
  if (!target) return;

  selectVertex(target);
  draggingVertex.value = {
    ...target,
    moved: false,
    pointerId: event.pointerId,
  };
  canvasRef.value?.setPointerCapture?.(event.pointerId);
}

function handlePointerMove(event: PointerEvent) {
  const dragging = draggingVertex.value;
  if (dragging) {
    const state = getPointerState(event, true);
    if (!state || !state.normalized) return;
    updateRulePoint(dragging, state.normalized);
    draggingVertex.value = { ...dragging, moved: true };
    return;
  }

  const state = getPointerState(event, false);
  if (!state) return;

  if (draftRule.value) {
    hoverPoint.value = state.normalized;
    renderCanvas();
  }
}

function handlePointerUp(event: PointerEvent) {
  const dragging = draggingVertex.value;
  if (!dragging || dragging.pointerId !== event.pointerId) return;
  suppressNextClick.value = dragging.moved;
  draggingVertex.value = null;
  canvasRef.value?.releasePointerCapture?.(event.pointerId);
}

function isSamePoint(first: NormalizedPoint, second: NormalizedPoint): boolean {
  return (
    Math.abs(first.x - second.x) <= POINT_EQUAL_EPSILON &&
    Math.abs(first.y - second.y) <= POINT_EQUAL_EPSILON
  );
}

function handleCanvasClick(event: MouseEvent) {
  if (suppressNextClick.value) {
    suppressNextClick.value = false;
    return;
  }

  const state = getPointerState(event);
  if (!state || !state.normalized) return;

  const target = getNearestVertex(state.local);
  const draft = draftRule.value;
  if (
    draft &&
    isAreaRole(draft.role) &&
    draft.points.length >= MIN_REGION_POINTS &&
    target?.draft &&
    target.pointIndex === 0
  ) {
    selectVertex(target);
    finishDraft();
    return;
  }

  if (target) {
    selectVertex(target);
    return;
  }
  if (!draft) return;

  addDraftPoint(state.normalized);
}

function handleCanvasDoubleClick(event: MouseEvent) {
  const draft = draftRule.value;
  if (!draft) return;

  const state = getPointerState(event);
  const target = state ? getNearestVertex(state.local) : null;
  const closesPolygon =
    isAreaRole(draft.role) &&
    draft.points.length >= MIN_REGION_POINTS &&
    target?.draft &&
    target.pointIndex === 0;
  const lastPoint = draft.points.at(-1);

  if (
    state?.normalized &&
    !closesPolygon &&
    (!lastPoint || !isSamePoint(lastPoint, state.normalized))
  ) {
    addDraftPoint(state.normalized);
  }
  finishDraft();
}

function handleCanvasLeave() {
  if (!draggingVertex.value) {
    hoverPoint.value = null;
    renderCanvas();
  }
}

function handleKeyDown(event: KeyboardEvent) {
  if (!props.open) return;
  if (event.key === 'Escape' && draftRule.value) {
    event.preventDefault();
    cancelDrawing();
    return;
  }
  if (
    (event.key === 'Backspace' || event.key === 'Delete') &&
    selectedVertex.value
  ) {
    event.preventDefault();
    removeSelectedVertex();
  }
}

function drawPath(
  context: CanvasRenderingContext2D,
  points: readonly NormalizedPoint[],
  close: boolean,
) {
  if (points.length === 0) return;
  const firstPoint = points[0];
  if (!firstPoint) return;
  const first = getCanvasPoint(firstPoint);
  context.beginPath();
  context.moveTo(first.x, first.y);
  for (const point of points.slice(1)) {
    const canvasPoint = getCanvasPoint(point);
    context.lineTo(canvasPoint.x, canvasPoint.y);
  }
  if (close) context.closePath();
}

function drawVertices(
  context: CanvasRenderingContext2D,
  points: readonly NormalizedPoint[],
  style: RuleStyle,
  target: Omit<VertexTarget, 'pointIndex'>,
) {
  points.forEach((point, pointIndex) => {
    const canvasPoint = getCanvasPoint(point);
    const selected =
      selectedVertex.value?.draft === target.draft &&
      selectedVertex.value?.ruleIndex === target.ruleIndex &&
      selectedVertex.value?.pointIndex === pointIndex;
    context.beginPath();
    context.arc(
      canvasPoint.x,
      canvasPoint.y,
      selected ? VERTEX_RADIUS_SELECTED : VERTEX_RADIUS_DEFAULT,
      0,
      Math.PI * 2,
    );
    context.fillStyle = selected ? '#ffffff' : style.stroke;
    context.fill();
    context.lineWidth = 2;
    context.strokeStyle = style.stroke;
    context.stroke();
  });
}

function drawArrow(
  context: CanvasRenderingContext2D,
  from: NormalizedPoint,
  to: NormalizedPoint,
  position: number,
  style: RuleStyle,
) {
  const start = getCanvasPoint(from);
  const end = getCanvasPoint(to);
  const dx = end.x - start.x;
  const dy = end.y - start.y;
  const length = Math.hypot(dx, dy);
  if (length === 0) return;

  const unitX = dx / length;
  const unitY = dy / length;
  const centerX = start.x + dx * position;
  const centerY = start.y + dy * position;
  const tipX = centerX + unitX * (ARROW_LENGTH / 2);
  const tipY = centerY + unitY * (ARROW_LENGTH / 2);
  const baseX = centerX - unitX * (ARROW_LENGTH / 2);
  const baseY = centerY - unitY * (ARROW_LENGTH / 2);
  const perpendicularX = -unitY * ARROW_WIDTH;
  const perpendicularY = unitX * ARROW_WIDTH;

  context.beginPath();
  context.moveTo(tipX, tipY);
  context.lineTo(baseX + perpendicularX, baseY + perpendicularY);
  context.lineTo(baseX - perpendicularX, baseY - perpendicularY);
  context.closePath();
  context.fillStyle = style.stroke;
  context.fill();
}

function drawDirectionArrows(
  context: CanvasRenderingContext2D,
  rule: TaskApi.DetectionRule,
  style: RuleStyle,
) {
  if (rule.points.length < MIN_LINE_POINTS) return;

  for (let index = 0; index < rule.points.length - 1; index += 1) {
    const from = rule.points[index];
    const to = rule.points[index + 1];
    if (!from || !to) continue;
    if (rule.lineDirection === LINE_DIRECTION.A_TO_B) {
      drawArrow(context, from, to, ARROW_POS_SINGLE, style);
    } else if (rule.lineDirection === LINE_DIRECTION.B_TO_A) {
      drawArrow(context, to, from, ARROW_POS_SINGLE, style);
    } else {
      drawArrow(context, from, to, ARROW_POS_FORWARD, style);
      drawArrow(context, to, from, 1 - ARROW_POS_BACKWARD, style);
    }
  }
}

function drawCompletedRule(
  context: CanvasRenderingContext2D,
  rule: TaskApi.DetectionRule,
  ruleIndex: number,
) {
  if (rule.points.length === 0) return;
  const style = getRuleStyle(rule.role);
  const selected = selectedRuleIndex.value === ruleIndex;

  context.save();
  context.lineWidth = selected ? 3 : 2;
  context.lineJoin = 'round';
  context.lineCap = 'round';
  context.strokeStyle = style.stroke;
  context.setLineDash(style.lineDash);
  drawPath(context, rule.points, isAreaRole(rule.role));
  if (isAreaRole(rule.role) && rule.points.length >= MIN_REGION_POINTS) {
    context.fillStyle = style.fill;
    context.fill();
  }
  context.stroke();
  if (rule.role === ROLE.LINE) {
    drawDirectionArrows(context, rule, style);
  }
  drawVertices(context, rule.points, style, {
    draft: false,
    ruleIndex,
  });
  context.restore();
}

function drawDraftRule(
  context: CanvasRenderingContext2D,
  rule: TaskApi.DetectionRule,
) {
  if (rule.points.length === 0) return;
  const style = getRuleStyle(rule.role);

  context.save();
  context.lineWidth = 2;
  context.lineJoin = 'round';
  context.lineCap = 'round';
  context.strokeStyle = style.stroke;
  context.setLineDash(DRAFT_LINE_DASH);

  const firstPoint = rule.points[0];
  if (firstPoint) {
    const first = getCanvasPoint(firstPoint);
    context.beginPath();
    context.moveTo(first.x, first.y);
    for (const point of rule.points.slice(1)) {
      const canvasPoint = getCanvasPoint(point);
      context.lineTo(canvasPoint.x, canvasPoint.y);
    }
    if (hoverPoint.value) {
      const hoverCanvas = getCanvasPoint(hoverPoint.value);
      context.lineTo(hoverCanvas.x, hoverCanvas.y);
    }
    context.stroke();
  }

  if (rule.role === ROLE.LINE) {
    drawDirectionArrows(context, rule, style);
  }
  drawVertices(context, rule.points, style, {
    draft: true,
    ruleIndex: -1,
  });
  context.restore();
}

function renderCanvas() {
  const canvas = canvasRef.value;
  if (!canvas || canvasSize.value.width <= 0 || canvasSize.value.height <= 0) {
    return;
  }

  const context = canvas.getContext('2d');
  if (!context) return;

  const width = canvasSize.value.width;
  const height = canvasSize.value.height;
  context.setTransform(
    devicePixelRatio.value,
    0,
    0,
    devicePixelRatio.value,
    0,
    0,
  );
  context.clearRect(0, 0, width, height);

  rules.value.forEach((rule, ruleIndex) => {
    drawCompletedRule(context, rule, ruleIndex);
  });
  if (draftRule.value) drawDraftRule(context, draftRule.value);
}

function resizeCanvas() {
  const canvas = canvasRef.value;
  if (!canvas) return;

  const metrics = getCanvasMetrics();
  if (!metrics) return;

  const { height, width } = metrics;
  canvasSize.value = { height, width };
  devicePixelRatio.value = window.devicePixelRatio || 1;
  canvas.width = Math.max(1, Math.floor(width * devicePixelRatio.value));
  canvas.height = Math.max(1, Math.floor(height * devicePixelRatio.value));
  renderCanvas();
}

function handleVideoMetadata(size: { height: number; width: number }) {
  if (!Number.isFinite(size.width) || !Number.isFinite(size.height)) return;
  if (size.width <= 0 || size.height <= 0) return;
  mediaWidth.value = size.width;
  mediaHeight.value = size.height;
  metadataReady.value = true;
  streamError.value = '';
  void nextTick(resizeCanvas);
}

function handleVideoError() {
  if (props.open) {
    streamError.value = $t('resource.task.ruleEditor.streamFailed');
  }
}

async function stopPreview(cameraId: number) {
  try {
    await stopLivePreviewApi(cameraId, STREAM_TYPE);
  } catch (error) {
    console.warn('Failed to stop detection rule preview:', error);
  }
}

async function releaseStream() {
  streamSession += 1;
  const activeCameraId = cameraNumericId.value;
  cameraNumericId.value = null;
  streamUrl.value = '';
  streamLoading.value = false;
  mediaWidth.value = 0;
  mediaHeight.value = 0;
  metadataReady.value = false;

  if (activeCameraId !== null) {
    await stopPreview(activeCameraId);
  }
}

async function startStream() {
  await releaseStream();
  const session = ++streamSession;
  streamLoading.value = true;
  streamError.value = '';

  try {
    const response = await getCameraPageApi({
      page: 1,
      pageSize: CAMERA_PAGE_MAX_SIZE,
    });
    if (session !== streamSession || !props.open) return;

    const camera = response.items.find(
      (item: CameraApi.CameraItem) => item.cameraId === props.cameraId,
    );
    if (!camera) {
      streamError.value = $t('resource.task.ruleEditor.cameraNotFound');
      return;
    }

    if (camera.lastWidth > 0 && camera.lastHeight > 0) {
      mediaWidth.value = camera.lastWidth;
      mediaHeight.value = camera.lastHeight;
    }

    const stream = await startLivePreviewApi(camera.id, STREAM_TYPE);
    const url = stream.wsUrl || stream.httpUrl;
    if (session !== streamSession || !props.open) {
      await stopPreview(camera.id);
      return;
    }
    if (!url) {
      await stopPreview(camera.id);
      throw new Error('preview stream url is empty');
    }

    cameraNumericId.value = camera.id;
    streamUrl.value = url;
  } catch {
    if (session === streamSession && props.open) {
      streamError.value = $t('resource.task.ruleEditor.streamFailed');
    }
  } finally {
    if (session === streamSession) {
      streamLoading.value = false;
    }
  }
}

function validate(): boolean {
  if (draftRule.value && !finishDraft()) {
    return false;
  }
  const issue = rules.value.map(validateRule).find(Boolean) ?? null;
  if (issue) {
    return false;
  }
  return true;
}

defineExpose({ validate });

watch(
  () => props.open,
  (isOpen) => {
    resetInteraction();
    if (isOpen) {
      void startStream();
    } else {
      void releaseStream();
    }
  },
  { immediate: true },
);

watch(
  () => props.cameraId,
  () => {
    if (props.open) void startStream();
  },
);

watch(rules, renderCanvas, { deep: true });
watch(draftRule, renderCanvas, { deep: true });
watch([selectedRuleIndex, selectedVertex], renderCanvas, { deep: true });
watch([mediaWidth, mediaHeight, streamUrl], () => {
  void nextTick(resizeCanvas);
});

onMounted(() => {
  window.addEventListener('keydown', handleKeyDown);
  if (typeof ResizeObserver !== 'undefined' && canvasRef.value) {
    resizeObserver = new ResizeObserver(() => resizeCanvas());
    resizeObserver.observe(canvasRef.value);
  }
  void nextTick(resizeCanvas);
});

onBeforeUnmount(() => {
  window.removeEventListener('keydown', handleKeyDown);
  resizeObserver?.disconnect();
  resizeObserver = null;
  resetInteraction();
  void releaseStream();
});
</script>

<template>
  <div class="detection-rule-editor space-y-3">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <div
        class="flex items-center gap-1 rounded-md border border-border bg-muted/40 p-1"
        role="tablist"
      >
        <button
          v-for="option in roleOptions"
          :key="option.role"
          type="button"
          role="tab"
          :aria-selected="draftRule?.role === option.role"
          :disabled="!canDraw"
          class="flex items-center gap-1.5 rounded px-2.5 py-1.5 text-xs transition-colors disabled:cursor-not-allowed disabled:opacity-40"
          :class="
            draftRule?.role === option.role
              ? 'bg-background text-foreground shadow-sm'
              : 'text-muted-foreground hover:bg-background/70 hover:text-foreground'
          "
          @click="startDrawing(option.role)"
        >
          <IconifyIcon :icon="option.icon" class="size-3.5" />
          <span>{{ getRoleLabel(option.role) }}</span>
        </button>
      </div>

      <div class="flex items-center gap-1">
        <Tooltip :title="$t('resource.task.ruleEditor.finish')">
          <Button
            html-type="button"
            type="text"
            size="small"
            :disabled="!draftRule"
            :aria-label="$t('resource.task.ruleEditor.finish')"
            @click="finishDraft"
          >
            <IconifyIcon icon="lucide:check" class="size-4" />
          </Button>
        </Tooltip>
        <Tooltip :title="$t('resource.task.ruleEditor.cancel')">
          <Button
            html-type="button"
            type="text"
            size="small"
            :disabled="!draftRule"
            :aria-label="$t('resource.task.ruleEditor.cancel')"
            @click="cancelDrawing"
          >
            <IconifyIcon icon="lucide:x" class="size-4" />
          </Button>
        </Tooltip>
        <Tooltip :title="$t('resource.task.ruleEditor.deletePoint')">
          <Button
            html-type="button"
            type="text"
            size="small"
            :disabled="!canDeleteSelectedVertex"
            :aria-label="$t('resource.task.ruleEditor.deletePoint')"
            @click="removeSelectedVertex"
          >
            <IconifyIcon icon="lucide:circle-minus" class="size-4" />
          </Button>
        </Tooltip>
        <Popconfirm
          :title="$t('resource.task.ruleEditor.clearConfirm')"
          :disabled="rules.length === 0 && !draftRule"
          @confirm="clearRules"
        >
          <Tooltip :title="$t('resource.task.ruleEditor.clear')">
            <Button
              html-type="button"
              type="text"
              size="small"
              danger
              :disabled="rules.length === 0 && !draftRule"
              :aria-label="$t('resource.task.ruleEditor.clear')"
            >
              <IconifyIcon icon="lucide:trash-2" class="size-4" />
            </Button>
          </Tooltip>
        </Popconfirm>
      </div>
    </div>

    <div
      class="relative min-h-[240px] w-full overflow-hidden rounded-md border border-slate-800 bg-slate-950 shadow-inner aspect-video"
    >
      <VideoPlayer
        :url="streamUrl"
        :title="props.cameraId"
        :show-controls="false"
        @error="handleVideoError"
        @metadata="handleVideoMetadata"
      />
      <canvas
        ref="canvasRef"
        class="absolute inset-0 z-10 h-full w-full touch-none"
        :class="canDraw ? 'cursor-crosshair' : 'pointer-events-none'"
        :aria-label="$t('resource.task.ruleEditor.canvasLabel')"
        @click="handleCanvasClick"
        @dblclick="handleCanvasDoubleClick"
        @pointercancel="handlePointerUp"
        @pointerdown="handlePointerDown"
        @pointerleave="handleCanvasLeave"
        @pointermove="handlePointerMove"
        @pointerup="handlePointerUp"
      ></canvas>

      <div
        v-if="streamLoading"
        class="pointer-events-none absolute inset-x-0 top-3 z-20 flex justify-center"
      >
        <div
          class="flex items-center gap-2 rounded-full border border-white/10 bg-black/65 px-3 py-1.5 text-xs text-white shadow-lg backdrop-blur"
        >
          <IconifyIcon
            icon="lucide:loader-circle"
            class="size-3.5 animate-spin"
          />
          <span>{{ $t('resource.task.ruleEditor.streamLoading') }}</span>
        </div>
      </div>
      <div
        v-if="streamError"
        class="pointer-events-none absolute inset-x-4 top-1/2 z-20 -translate-y-1/2"
      >
        <Alert :message="streamError" type="error" show-icon />
      </div>
      <div
        v-else-if="streamUrl && !canDraw"
        class="pointer-events-none absolute inset-x-0 bottom-3 z-20 flex justify-center"
      >
        <span
          class="rounded-full border border-white/10 bg-black/60 px-3 py-1 text-xs text-white/80 backdrop-blur"
        >
          {{ $t('resource.task.ruleEditor.waitingMetadata') }}
        </span>
      </div>
    </div>

    <div class="flex flex-wrap items-center justify-between gap-2 text-xs">
      <div class="flex flex-wrap items-center gap-x-4 gap-y-1.5">
        <span class="font-medium text-foreground">
          {{ $t('resource.task.ruleEditor.legend') }}
        </span>
        <span class="flex items-center gap-1.5 text-muted-foreground">
          <i class="size-2.5 rounded-full bg-cyan-400"></i>
          {{ getRoleLabel(ROLE.ROI) }}
        </span>
        <span class="flex items-center gap-1.5 text-muted-foreground">
          <i class="size-2.5 rounded-full bg-orange-400"></i>
          {{ getRoleLabel(ROLE.MASK) }}
        </span>
        <span class="flex items-center gap-1.5 text-muted-foreground">
          <i class="size-2.5 rounded-full bg-rose-400"></i>
          {{ getRoleLabel(ROLE.LINE) }}
        </span>
      </div>
      <span
        v-if="draftRule"
        class="flex items-center gap-1.5 font-medium text-primary"
      >
        <IconifyIcon icon="lucide:pencil" class="size-3.5" />
        {{
          $t('resource.task.ruleEditor.drawing', {
            role: getRoleLabel(draftRule.role),
          })
        }}
      </span>
    </div>

    <Alert
      v-if="validationMessage"
      :message="validationMessage"
      type="error"
      show-icon
    />

    <div class="rounded-md border border-border bg-muted/20">
      <div
        class="flex items-center justify-between border-b border-border px-3 py-2"
      >
        <span class="text-xs font-medium text-foreground">
          {{ $t('resource.task.ruleEditor.ruleList') }}
        </span>
        <Tag v-if="rules.length" color="blue">{{ ruleCountLabel }}</Tag>
      </div>
      <div v-if="rules.length" class="divide-y divide-border">
        <div
          v-for="(rule, index) in rules"
          :key="`rule-${index}`"
          class="cursor-pointer px-3 py-2.5 transition-colors"
          :class="
            selectedRuleIndex === index ? 'bg-primary/5' : 'hover:bg-accent/40'
          "
          @click="selectRule(index)"
        >
          <div class="flex flex-wrap items-center gap-2">
            <span
              class="size-2.5 shrink-0 rounded-full"
              :style="{ backgroundColor: getRuleStyle(rule.role).stroke }"
            ></span>
            <span class="text-xs font-medium">{{
              getRoleLabel(rule.role)
            }}</span>
            <span class="text-muted-foreground text-[11px]">
              {{
                $t('resource.task.ruleEditor.ruleNumber', { index: index + 1 })
              }}
            </span>
            <span class="text-muted-foreground text-[11px]">
              {{
                $t('resource.task.ruleEditor.points', {
                  count: rule.points.length,
                })
              }}
            </span>
            <span
              v-if="rule.role === ROLE.LINE"
              class="text-muted-foreground text-[11px]"
            >
              {{ getDirectionLabel(rule.lineDirection) }}
            </span>
            <span class="flex-1"></span>
            <Select
              v-if="rule.role === ROLE.LINE"
              size="small"
              :value="rule.lineDirection"
              :options="directionOptions"
              class="min-w-[112px]"
              @click.stop
              @update:value="(value) => updateLineDirection(index, value)"
            />
            <Tooltip :title="$t('resource.task.ruleEditor.deleteRule')">
              <Button
                html-type="button"
                type="text"
                danger
                size="small"
                :aria-label="$t('resource.task.ruleEditor.deleteRule')"
                @click.stop="deleteRule(index)"
              >
                <IconifyIcon icon="lucide:trash-2" class="size-3.5" />
              </Button>
            </Tooltip>
          </div>
        </div>
      </div>
      <Empty
        v-else
        :image="Empty.PRESENTED_IMAGE_SIMPLE"
        :description="$t('resource.task.ruleEditor.empty')"
        class="my-5"
      />
    </div>
  </div>
</template>
