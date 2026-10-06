-- SPC 初始 schema（PostgreSQL 16）

CREATE TABLE targets (
    id             BIGSERIAL    PRIMARY KEY,
    name           TEXT        NOT NULL,
    machine        TEXT        NOT NULL,
    dimension      TEXT        NOT NULL,
    usl            DOUBLE PRECISION,
    lsl            DOUBLE PRECISION,
    subgroup_n     INTEGER     NOT NULL CHECK (subgroup_n BETWEEN 2 AND 10),
    enabled_rules  INTEGER[]   NOT NULL DEFAULT '{1,2,3,4}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT targets_spec_side CHECK (usl IS NOT NULL OR lsl IS NOT NULL),
    CONSTRAINT targets_spec_order CHECK (usl IS NULL OR lsl IS NULL OR usl > lsl)
);

-- 测量点全局入序：target_id + seq 即「服务器收到的点序」。
-- subgroup_seq 为空表示尚未凑满一个子组。
CREATE TABLE measurements (
    id            BIGSERIAL    PRIMARY KEY,
    target_id     BIGINT       NOT NULL REFERENCES targets(id),
    seq           BIGINT       NOT NULL,
    value         DOUBLE PRECISION NOT NULL,
    subgroup_seq  INTEGER,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT measurements_seq_uniq UNIQUE (target_id, seq)
);

CREATE TABLE subgroups (
    target_id     BIGINT       NOT NULL REFERENCES targets(id),
    seq           INTEGER      NOT NULL,
    mean          DOUBLE PRECISION NOT NULL,
    range         DOUBLE PRECISION NOT NULL,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (target_id, seq)
);

-- 冻结的控制限，每次显式发起基准产生一行；旧行留档。
CREATE TABLE baselines (
    id             BIGSERIAL    PRIMARY KEY,
    target_id      BIGINT       NOT NULL REFERENCES targets(id),
    version        INTEGER      NOT NULL,
    ref_start_seq  INTEGER      NOT NULL,
    ref_end_seq    INTEGER      NOT NULL,
    effective_from INTEGER      NOT NULL,
    effective_to   INTEGER,
    xbar_bar       DOUBLE PRECISION NOT NULL,
    rbar           DOUBLE PRECISION NOT NULL,
    ucl_x          DOUBLE PRECISION NOT NULL,
    lcl_x          DOUBLE PRECISION NOT NULL,
    ucl_r          DOUBLE PRECISION NOT NULL,
    lcl_r          DOUBLE PRECISION NOT NULL,
    sigma_within   DOUBLE PRECISION NOT NULL,
    cp             DOUBLE PRECISION,
    cpk            DOUBLE PRECISION,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT baselines_target_ver UNIQUE (target_id, version)
);

-- 告警只增不改不删（重新基准不删除，仅后续新限产生新告警）。
CREATE TABLE alarms (
    target_id     BIGINT      NOT NULL REFERENCES targets(id),
    rule_no       INTEGER     NOT NULL,
    trigger_seq   INTEGER     NOT NULL,
    involved_seq  INTEGER[]   NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (target_id, rule_no, trigger_seq)
);

CREATE INDEX idx_measurements_pending ON measurements(target_id, seq)
    WHERE subgroup_seq IS NULL;
CREATE INDEX idx_subgroups_target ON subgroups(target_id, seq);
CREATE INDEX idx_baselines_target ON baselines(target_id, version);
CREATE INDEX idx_alarms_target ON alarms(target_id, trigger_seq);
