-- 000021_add_alarm_records.down.sql
-- 回滚：删除 alarm_records 表及索引

DROP TABLE IF EXISTS alarm_records;
