import { $t } from '@vben/locales';

/**
 * translateMenuLabel 解析后端存入的 i18n key（如 system.user.addUser / routes.system.user），
 * 非 key（ASCII 路由标识符、自定义文本）原样返回（对齐操作日志 formatAction 降级策略）。
 */
function translateMenuLabel(value?: string) {
  if (!value) return '';
  if (value.startsWith('system.') || value.startsWith('routes.')) {
    const translated = $t(value);
    if (translated && translated !== value) return translated;
  }
  return value;
}

export { translateMenuLabel };
