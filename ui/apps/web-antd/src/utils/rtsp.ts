/**
 * RTSP URL 解析/编码工具（摄像头表单专用）。
 *
 * 规则（见 PRD R1.7「自动解析 + 可见校正」）：
 * - 以最后一个 `@` 作为 userinfo 与 host 的确定性分割线；
 * - userinfo 内按第一个 `:` 拆用户名/密码；
 * - 密码中的 `@`、`/`、`#`、`?` 等未编码字符只要位于最后一个 `@` 之前就不会破坏拆分；
 * - 提交/测活前对用户名/密码做百分号编码并拼回完整 URL（后端只接收/保存单一完整 rtspUrl）；
 * - 编辑时反向解析并解码 userinfo 填充临时字段。
 */

export interface RtspParts {
  /** 无凭据 RTSP 地址（rtsp://host[:port]/path），不含 userinfo */
  address: string;
  username: string;
  password: string;
}

/** 安全解码：非法百分号编码时原样返回，不抛异常。 */
function safeDecode(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

/**
 * 解析完整 RTSP URL，拆分为无凭据地址 + 用户名 + 密码。
 * 无 userinfo 时返回原地址与空凭据。
 */
export function parseRtspUrl(url: string): RtspParts {
  const trimmed = url.trim();
  const atIndex = trimmed.lastIndexOf('@');
  if (atIndex <= 0) {
    return { address: trimmed, username: '', password: '' };
  }
  const beforeAt = trimmed.slice(0, atIndex);
  // userinfo 从 scheme 分隔符（://）之后开始，避免把 rtsp:// 里的冒号当作凭据分隔。
  const schemeEnd = beforeAt.indexOf('://');
  const scheme = schemeEnd === -1 ? '' : beforeAt.slice(0, schemeEnd + 3);
  const userinfo = schemeEnd === -1 ? beforeAt : beforeAt.slice(schemeEnd + 3);
  const rest = trimmed.slice(atIndex + 1);

  const colonIndex = userinfo.indexOf(':');
  const rawUsername =
    colonIndex === -1 ? userinfo : userinfo.slice(0, colonIndex);
  const rawPassword = colonIndex === -1 ? '' : userinfo.slice(colonIndex + 1);
  return {
    address: `${scheme}${rest}`,
    username: safeDecode(rawUsername),
    password: safeDecode(rawPassword),
  };
}

/**
 * 将无凭据地址 + 用户名/密码拼回完整 RTSP URL（百分号编码凭据）。
 * 若 address 已含 userinfo（用户粘贴完整 URL），按最后一个 `@` 规则剥离后重拼。
 */
export function buildRtspUrl(
  address: string,
  username: string,
  password: string,
): string {
  const base = address.trim();
  const schemeIndex = base.indexOf('://');
  if (schemeIndex === -1) {
    return base;
  }
  const scheme = base.slice(0, schemeIndex + 3);
  let rest = base.slice(schemeIndex + 3);
  // 地址可能已含 userinfo（防御：剥离后重拼）
  if (rest.includes('@')) {
    rest = rest.slice(rest.lastIndexOf('@') + 1);
  }
  if (username === '' && password === '') {
    return `${scheme}${rest}`;
  }
  const userinfo = `${encodeURIComponent(username)}:${encodeURIComponent(password)}`;
  return `${scheme}${userinfo}@${rest}`;
}
