import { $t } from '@vben/locales';

// 后端存入的 i18n key 前缀（与各 locale 的 namespace 对齐：system / routes / auth / ops / resource / live / ai / record）。
const I18N_KEY_PREFIXES = [
  'system.',
  'routes.',
  'auth.',
  'ops.',
  'resource.',
  'live.',
  'ai.',
  'record.',
];

/**
 * translateI18nKey 若 value 是已知前缀的 i18n key 则尝试翻译，否则原样返回。
 * 菜单标题与操作日志动作的降级策略共用（对齐 code-reuse-thinking-guide 模式 4）。
 */
function translateI18nKey(value: string): string {
  if (I18N_KEY_PREFIXES.some((prefix) => value.startsWith(prefix))) {
    const translated = $t(value);
    if (translated && translated !== value) return translated;
  }
  return value;
}

/**
 * 获取算法名称的本地化文本。内置算法优先从 ai.algorithm.builtInAlgos.<id>.name 匹配，
 * 若未配置或为第三方自定义算法包则回退至后端返回的 name 或 algorithmId。
 */
function formatAlgorithmName(
  algorithmId?: string,
  fallbackName?: string,
): string {
  if (!algorithmId) return fallbackName || '';
  const key = `ai.algorithm.builtInAlgos.${algorithmId}.name`;
  const translated = $t(key);
  if (translated && translated !== key) return translated;
  return fallbackName || algorithmId;
}

/**
 * 获取算法描述的本地化文本。内置算法优先从 ai.algorithm.builtInAlgos.<id>.description 匹配。
 */
function formatAlgorithmDesc(
  algorithmId?: string,
  fallbackDesc?: string,
): string {
  if (!algorithmId) return fallbackDesc || '';
  const key = `ai.algorithm.builtInAlgos.${algorithmId}.description`;
  const translated = $t(key);
  if (translated && translated !== key) return translated;
  return fallbackDesc || '';
}

/**
 * 获取告警事件类型名称的本地化文本，优先从 ai.alarmTypes.<type> 或 record.alarm.types.<type> 匹配。
 */
function formatAlarmTypeName(alarmTypeId?: string): string {
  if (!alarmTypeId) return '';
  const aiKey = `ai.alarmTypes.${alarmTypeId}`;
  const aiTranslated = $t(aiKey);
  if (aiTranslated && aiTranslated !== aiKey) return aiTranslated;

  const recordKey = `record.alarm.types.${alarmTypeId}`;
  const recordTranslated = $t(recordKey);
  if (recordTranslated && recordTranslated !== recordKey)
    return recordTranslated;

  return alarmTypeId;
}

/**
 * 获取目标类别的本地化展示，例如 "人 (person)"。
 */
function formatTargetClass(targetClass?: string): string {
  if (!targetClass) return 'Target';
  const key = `ai.classes.${targetClass}`;
  const translated = $t(key);
  return translated && translated !== key
    ? `${translated} (${targetClass})`
    : targetClass;
}

export {
  formatAlarmTypeName,
  formatAlgorithmDesc,
  formatAlgorithmName,
  formatTargetClass,
  translateI18nKey,
};
