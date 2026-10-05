-- 完成核验轮次与 Result（任务编排 4.7）。

-- 目标推进冻结：持有冻结的轮次号；冻结只存在于该轮次为 VERIFYING 期间。
ALTER TABLE tasks ADD COLUMN frozen_round INTEGER NOT NULL DEFAULT 0;

CREATE TABLE verification_rounds (
    user_id  TEXT    NOT NULL,
    task_id  TEXT    NOT NULL,
    round_no INTEGER NOT NULL,
    status   INTEGER NOT NULL,
    record   BLOB    NOT NULL,
    PRIMARY KEY (user_id, task_id, round_no)
);

-- Result：与任务关闭在同一事务写入，之后不再修改。
CREATE TABLE results (
    user_id   TEXT    NOT NULL,
    task_id   TEXT    NOT NULL,
    outcome   INTEGER NOT NULL,
    record    BLOB    NOT NULL,
    closed_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, task_id)
);
