-- 冻结子组与告警在判定时刻归属的控制限版本。
--
-- 旧实现只在查看时按 baselines.effective_* 现算归属；重新基准若取中间基准期，
-- 会把已经判定的子组和告警改挂到新限。本迁移：
--   1. 修正历史重新基准的生效起点：发起时已存在的最后一个完整子组之后；
--   2. 删除旧限切换后被错误回溯补出的告警；
--   3. 将子组、告警归属固化到 baseline_id；
--   4. 告警幂等键改为限版本内的 (target, baseline, rule, trigger)。

ALTER TABLE subgroups
    ADD COLUMN IF NOT EXISTS baseline_id BIGINT;

ALTER TABLE alarms
    ADD COLUMN IF NOT EXISTS baseline_id BIGINT;

-- 修正 v2+ 的生效区间。旧版本代码把 effective_from 写成 ref_end_seq+1；
-- 正确语义是重新基准请求获得目标行锁时，最后一个已完整提交子组的下一组。
-- 目标行锁保证基准与录入串行，因此按创建时间可恢复该边界。
UPDATE baselines b
SET effective_from = GREATEST(
    b.ref_end_seq + 1,
    COALESCE((
        SELECT max(s.seq) + 1
        FROM subgroups s
        WHERE s.target_id = b.target_id
          AND s.created_at < b.created_at
    ), b.ref_end_seq + 1)
)
WHERE b.version > 1;

-- 每个旧版本生效到下一版起点前一组；最新版本继续开放。
UPDATE baselines old
SET effective_to = next.effective_from - 1
FROM baselines next
WHERE next.target_id = old.target_id
  AND next.version = old.version + 1;

-- 删除重新基准后、新子组录入时被错误回溯补出的告警。
-- 旧全局主键 (target, rule, trigger) 会挡住与旧告警同键的重复项；
-- 因此这里只删除在该版创建之后才出现、且触发点落在其正确生效起点之前的新增项。
DELETE FROM alarms a
USING baselines b
LEFT JOIN baselines prev
  ON prev.target_id = b.target_id
 AND prev.version = b.version - 1
LEFT JOIN baselines nb
  ON nb.target_id = b.target_id
 AND nb.version = b.version + 1
WHERE a.target_id = b.target_id
  AND b.version > 1
  AND a.created_at >= b.created_at
  AND (nb.id IS NULL OR a.created_at < nb.created_at)
  AND (
       a.trigger_seq < b.effective_from
       OR (
           a.trigger_seq >= prev.effective_from
           AND a.trigger_seq <= COALESCE(prev.effective_to, 2147483647)
       )
  );

-- 子组归属按修正后的不重叠接管区间固化；首版基准参考点保持 NULL。
-- 不能只按区间匹配：重新基准的参考期（如 11..30）仍属于当时已经判定的旧限，
-- 不能因为它落在新基准 ref_start/ref_end 内就置空。
UPDATE subgroups s
SET baseline_id = COALESCE((
    SELECT b.id
    FROM baselines b
    WHERE b.target_id = s.target_id
      AND b.version = 1
      AND s.seq >= b.effective_from
), (
    SELECT b.id
    FROM baselines b
    WHERE b.target_id = s.target_id
      AND s.seq >= b.effective_from
      AND s.seq <= COALESCE(b.effective_to, 2147483647)
    ORDER BY b.version
    LIMIT 1
))
WHERE s.baseline_id IS NULL;

-- 告警归属以触发子组当时的接管区间固化；区间已经修正为互不重叠。
UPDATE alarms a
SET baseline_id = (
    SELECT b.id
    FROM baselines b
    WHERE b.target_id = a.target_id
      AND a.trigger_seq >= b.effective_from
      AND a.trigger_seq <= COALESCE(b.effective_to, 2147483647)
    ORDER BY b.version
    LIMIT 1
);

CREATE INDEX IF NOT EXISTS idx_subgroups_baseline
    ON subgroups(target_id, baseline_id);
DROP INDEX IF EXISTS idx_alarms_target;
CREATE INDEX idx_alarms_target
    ON alarms(target_id, baseline_id, trigger_seq);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'subgroups_baseline_fkey'
    ) THEN
        ALTER TABLE subgroups
            ADD CONSTRAINT subgroups_baseline_fkey
            FOREIGN KEY (baseline_id) REFERENCES baselines(id);
    END IF;
END $$;

DO $$
DECLARE
    has_baseline_fk boolean;
BEGIN
    SELECT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'alarms_baseline_fkey'
    ) INTO has_baseline_fk;
    IF NOT has_baseline_fk THEN
        ALTER TABLE alarms
            ADD CONSTRAINT alarms_baseline_fkey
            FOREIGN KEY (baseline_id) REFERENCES baselines(id);
    END IF;

    ALTER TABLE alarms ALTER COLUMN baseline_id SET NOT NULL;

    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'alarms'::regclass
          AND contype = 'p'
          AND pg_get_constraintdef(oid) NOT LIKE '%baseline_id%'
    ) THEN
        ALTER TABLE alarms DROP CONSTRAINT alarms_pkey;
        ALTER TABLE alarms
            ADD PRIMARY KEY (target_id, baseline_id, rule_no, trigger_seq);
    END IF;
END $$;
