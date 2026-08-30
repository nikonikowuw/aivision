export interface NormalizedPoint {
  x: number;
  y: number;
}

export interface VideoViewport {
  elementHeight: number;
  elementWidth: number;
  videoHeight: number;
  videoWidth: number;
}

export interface RenderedVideoRect {
  height: number;
  offsetX: number;
  offsetY: number;
  width: number;
}

export interface CanvasPoint {
  x: number;
  y: number;
}

const NORMALIZED_MAX = 1;
const NORMALIZED_MIN = 0;
const GEOMETRY_EPSILON = 1e-9;

/**
 * 计算 object-fit: contain 下视频有效画面的 CSS 区域。
 * 坐标原点必须落在有效画面左上角，不能直接使用容器尺寸归一化。
 */
export function getRenderedVideoRect(
  viewport: VideoViewport,
): RenderedVideoRect {
  const elementWidth = Math.max(0, viewport.elementWidth);
  const elementHeight = Math.max(0, viewport.elementHeight);
  const videoWidth = Math.max(0, viewport.videoWidth);
  const videoHeight = Math.max(0, viewport.videoHeight);

  if (
    elementWidth === 0 ||
    elementHeight === 0 ||
    videoWidth === 0 ||
    videoHeight === 0
  ) {
    return { height: 0, offsetX: 0, offsetY: 0, width: 0 };
  }

  const videoAspectRatio = videoWidth / videoHeight;
  const elementAspectRatio = elementWidth / elementHeight;

  if (videoAspectRatio > elementAspectRatio) {
    const width = elementWidth;
    const height = width / videoAspectRatio;
    return {
      height,
      offsetX: 0,
      offsetY: (elementHeight - height) / 2,
      width,
    };
  }

  const height = elementHeight;
  const width = height * videoAspectRatio;
  return {
    height,
    offsetX: (elementWidth - width) / 2,
    offsetY: 0,
    width,
  };
}

/**
 * 将容器内 CSS 像素坐标转换为视频有效画面内的归一化坐标。
 * @param clampToViewport 是否在越界（黑边）时钳制到 [0, 1] 区间（拖拽时设为 true 以贴边；点击拾取时设为 false 忽略黑边）
 */
export function normalizePoint(
  point: CanvasPoint,
  viewport: VideoViewport,
  clampToViewport = false,
): NormalizedPoint | null {
  const rendered = getRenderedVideoRect(viewport);
  if (rendered.width === 0 || rendered.height === 0) return null;

  const right = rendered.offsetX + rendered.width;
  const bottom = rendered.offsetY + rendered.height;
  const isOutOfBounds =
    point.x < rendered.offsetX ||
    point.x > right ||
    point.y < rendered.offsetY ||
    point.y > bottom;

  if (isOutOfBounds && !clampToViewport) {
    return null;
  }

  return {
    x: clampNormalized((point.x - rendered.offsetX) / rendered.width),
    y: clampNormalized((point.y - rendered.offsetY) / rendered.height),
  };
}

/** 将归一化坐标反算为容器内 CSS 像素坐标。 */
export function denormalizePoint(
  point: NormalizedPoint,
  viewport: VideoViewport,
): CanvasPoint {
  const rendered = getRenderedVideoRect(viewport);
  return {
    x: point.x * rendered.width + rendered.offsetX,
    y: point.y * rendered.height + rendered.offsetY,
  };
}

/** 限制外部数据或浮点误差落在协议允许的 [0, 1] 区间。 */
export function clampNormalized(value: number): number {
  return Math.min(NORMALIZED_MAX, Math.max(NORMALIZED_MIN, value));
}

/**
 * 判断闭合多边形是否自交。相邻边共享端点属于合法情况，非相邻边相交则返回 true。
 */
export function hasSelfIntersection(
  points: readonly NormalizedPoint[],
): boolean {
  const pointCount = points.length;
  if (pointCount < 4) return false;

  for (let firstEdge = 0; firstEdge < pointCount; firstEdge += 1) {
    for (
      let secondEdge = firstEdge + 1;
      secondEdge < pointCount;
      secondEdge += 1
    ) {
      if (
        secondEdge === firstEdge + 1 ||
        (firstEdge === 0 && secondEdge === pointCount - 1)
      ) {
        continue;
      }

      const firstEnd = (firstEdge + 1) % pointCount;
      const secondEnd = (secondEdge + 1) % pointCount;
      const firstStartPoint = points[firstEdge];
      const firstEndPoint = points[firstEnd];
      const secondStartPoint = points[secondEdge];
      const secondEndPoint = points[secondEnd];
      if (
        !firstStartPoint ||
        !firstEndPoint ||
        !secondStartPoint ||
        !secondEndPoint
      ) {
        continue;
      }
      if (
        segmentsIntersect(
          firstStartPoint,
          firstEndPoint,
          secondStartPoint,
          secondEndPoint,
        )
      ) {
        return true;
      }
    }
  }

  return false;
}

function cross(
  origin: NormalizedPoint,
  first: NormalizedPoint,
  second: NormalizedPoint,
): number {
  return (
    (first.x - origin.x) * (second.y - origin.y) -
    (first.y - origin.y) * (second.x - origin.x)
  );
}

function isOnSegment(
  start: NormalizedPoint,
  point: NormalizedPoint,
  end: NormalizedPoint,
): boolean {
  return (
    point.x >= Math.min(start.x, end.x) - GEOMETRY_EPSILON &&
    point.x <= Math.max(start.x, end.x) + GEOMETRY_EPSILON &&
    point.y >= Math.min(start.y, end.y) - GEOMETRY_EPSILON &&
    point.y <= Math.max(start.y, end.y) + GEOMETRY_EPSILON
  );
}

function segmentsIntersect(
  firstStart: NormalizedPoint,
  firstEnd: NormalizedPoint,
  secondStart: NormalizedPoint,
  secondEnd: NormalizedPoint,
): boolean {
  const firstAgainstSecondStart = cross(secondStart, secondEnd, firstStart);
  const firstAgainstSecondEnd = cross(secondStart, secondEnd, firstEnd);
  const secondAgainstFirstStart = cross(firstStart, firstEnd, secondStart);
  const secondAgainstFirstEnd = cross(firstStart, firstEnd, secondEnd);

  const firstStrictlyCrosses =
    ((firstAgainstSecondStart > GEOMETRY_EPSILON &&
      firstAgainstSecondEnd < -GEOMETRY_EPSILON) ||
      (firstAgainstSecondStart < -GEOMETRY_EPSILON &&
        firstAgainstSecondEnd > GEOMETRY_EPSILON)) &&
    ((secondAgainstFirstStart > GEOMETRY_EPSILON &&
      secondAgainstFirstEnd < -GEOMETRY_EPSILON) ||
      (secondAgainstFirstStart < -GEOMETRY_EPSILON &&
        secondAgainstFirstEnd > GEOMETRY_EPSILON));
  if (firstStrictlyCrosses) return true;

  return (
    (Math.abs(firstAgainstSecondStart) <= GEOMETRY_EPSILON &&
      isOnSegment(secondStart, firstStart, secondEnd)) ||
    (Math.abs(firstAgainstSecondEnd) <= GEOMETRY_EPSILON &&
      isOnSegment(secondStart, firstEnd, secondEnd)) ||
    (Math.abs(secondAgainstFirstStart) <= GEOMETRY_EPSILON &&
      isOnSegment(firstStart, secondStart, firstEnd)) ||
    (Math.abs(secondAgainstFirstEnd) <= GEOMETRY_EPSILON &&
      isOnSegment(firstStart, secondEnd, firstEnd))
  );
}
