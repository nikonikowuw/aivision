import { $t } from '@vben/locales';

import { message } from 'ant-design-vue';

/**
 * 降级复制方案，兼容非 HTTPS 或不支持 Clipboard API 的受限环境
 */
export function fallbackCopy(text: string, successMsg?: string) {
  try {
    const textArea = document.createElement('textarea');
    textArea.value = text;
    textArea.style.position = 'fixed';
    textArea.style.opacity = '0';
    document.body.append(textArea);
    textArea.focus();
    textArea.select();
    const successful = document.execCommand('copy');
    textArea.remove();
    if (successful) {
      message.success(
        successMsg || $t('system.common.copySuccess') || '复制成功',
      );
      return;
    }
  } catch (error) {
    console.error('Fallback copy failed:', error);
  }
  message.info(text);
}

/**
 * 复制文本到剪贴板，优先使用 navigator.clipboard，失败时降级
 */
export async function copyToClipboard(text?: string, successMsg?: string) {
  if (!text) return;
  const msg = successMsg || $t('system.common.copySuccess') || '复制成功';
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      message.success(msg);
    } catch {
      fallbackCopy(text, msg);
    }
  } else {
    fallbackCopy(text, msg);
  }
}
