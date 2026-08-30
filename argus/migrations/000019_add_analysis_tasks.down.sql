-- 000019_add_analysis_tasks.down.sql
-- 回滚：按依赖逆序删除任务配置模块三张表。

DROP TABLE IF EXISTS desired_state_revision;
DROP TABLE IF EXISTS algorithm_instances;
DROP TABLE IF EXISTS analysis_tasks;
