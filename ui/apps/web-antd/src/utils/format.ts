/**
 * 根据置信度或相似度数值（0.0 ~ 1.0）获取统一的 Ant Design Tag 语义颜色。
 */
export function getConfidenceTagColor(
  val?: number,
): 'blue' | 'default' | 'green' | 'orange' | 'red' {
  if (typeof val !== 'number' || !Number.isFinite(val)) return 'default';
  if (val >= 0.9) return 'green';
  if (val >= 0.75) return 'blue';
  if (val >= 0.6) return 'orange';
  return 'red';
}

/**
 * 根据多态目标类型获取标签语义颜色。
 */
export function getTargetTypeTagColor(
  targetType?: string,
): 'blue' | 'cyan' | 'default' | 'green' | 'orange' {
  switch (targetType) {
    case 'face': {
      return 'cyan';
    }
    case 'person': {
      return 'blue';
    }
    case 'vehicle': {
      return 'orange';
    }
    case 'non_motor': {
      return 'green';
    }
    default: {
      return 'default';
    }
  }
}
