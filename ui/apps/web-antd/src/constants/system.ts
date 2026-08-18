export const SYSTEM_STATUS = {
  DISABLED: 0,
  ENABLED: 1,
} as const;

export const SYSTEM_ADMIN_USER_ID = 1;
export const SYSTEM_ADMIN_USERNAME = 'admin';
export const SYSTEM_BUILTIN_ROLE_ID = 1;
export const SYSTEM_ROLE_ADMIN_CODE = 'admin';
export const SYSTEM_ROLE_SUPER_CODE = 'super';

export type SystemStatus = (typeof SYSTEM_STATUS)[keyof typeof SYSTEM_STATUS];
