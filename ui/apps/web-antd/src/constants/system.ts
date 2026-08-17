export const SYSTEM_STATUS = {
  DISABLED: 0,
  ENABLED: 1,
} as const;

export type SystemStatus = (typeof SYSTEM_STATUS)[keyof typeof SYSTEM_STATUS];
