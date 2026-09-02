import dayjs from 'dayjs';

/**
 * 获取今日起止时间范围 [YYYY-MM-DDTHH:mm:ssZ, YYYY-MM-DDTHH:mm:ssZ]
 */
export function getTodayRange(): [string, string] {
  const startOfDay = dayjs().startOf('day').format('YYYY-MM-DDTHH:mm:ssZ');
  const endOfDay = dayjs().endOf('day').format('YYYY-MM-DDTHH:mm:ssZ');
  return [startOfDay, endOfDay];
}
