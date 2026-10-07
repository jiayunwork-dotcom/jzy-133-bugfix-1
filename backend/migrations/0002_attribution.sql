-- 「判定归属落库」：子组与告警在判定那一刻归属哪一套冻结限，当场记下，
-- 之后重新基准、基准期跨边界、服务重启都不再改变归属。
--
-- 重新基准的基准期可以跨过旧限的生效区间（如 11~30），因此归属不能在
-- 查看时按 effective 区间现算——会把 21~30 这些「曾按旧限判定」的点算成
-- 新限的参考点，也会把 31~40 改挂到新限名下。归属必须持久化。

ALTER TABLE subgroups
    ADD COLUMN baseline_id BIGINT REFERENCES baselines(id);

ALTER TABLE alarms
    ADD COLUMN baseline_id BIGINT REFERENCES baselines(id);

-- 历史数据回填（仅针对升级前已存在的库）：按判定时点的生效区间归属，
-- 同一组号若被多段区间覆盖取版本最早的一段。
--
-- 固有局限（无法消除）：旧 schema 不记录判定归属，像「重基前已按旧限判定、
-- 而那次重基把基准期取在旧限生效区间中间」的点，其旧限归属事实在旧数据里
-- 已丢失，纯 SQL 无法恢复——回填只能重现旧代码当时按区间现算的显示结果。
-- 这类点升级后仍显示为新限归属，直到按新代码重新走一遍录入/重基流程才会
-- 获得准确归属。0002 之后新写入的数据归属由切组事务当场落库，完全准确。
--
-- 基准期恰好在末尾、从未被任何生效区间覆盖的参考点保持 NULL；读侧的
-- 「参考点」标记始终由参考期成员关系（ref_start/end_seq）单独推导，与这
-- 一列是否为 NULL 无关。
UPDATE subgroups sg SET baseline_id = bl.id
FROM baselines bl
WHERE sg.target_id = bl.target_id
  AND sg.baseline_id IS NULL
  AND sg.seq >= bl.effective_from
  AND sg.seq <= COALESCE(bl.effective_to, 2147483647)
  AND NOT EXISTS (
      SELECT 1 FROM baselines b2
      WHERE b2.target_id = bl.target_id AND b2.version < bl.version
        AND sg.seq >= b2.effective_from
        AND sg.seq <= COALESCE(b2.effective_to, 2147483647)
  );

UPDATE alarms al SET baseline_id = bl.id
FROM baselines bl
WHERE al.target_id = bl.target_id
  AND al.baseline_id IS NULL
  AND al.trigger_seq >= bl.effective_from
  AND al.trigger_seq <= COALESCE(bl.effective_to, 2147483647)
  AND NOT EXISTS (
      SELECT 1 FROM baselines b2
      WHERE b2.target_id = bl.target_id AND b2.version < bl.version
        AND al.trigger_seq >= b2.effective_from
        AND al.trigger_seq <= COALESCE(b2.effective_to, 2147483647)
  );

CREATE INDEX idx_subgroups_baseline ON subgroups(target_id, baseline_id);
CREATE INDEX idx_alarms_baseline ON alarms(target_id, baseline_id);
