import { $t } from '@vben/locales';

// 后端存入的 i18n key 前缀（与各 locale 的 namespace 对齐：system / routes / auth / ops / resource / live / ai）。
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

export { translateI18nKey };
