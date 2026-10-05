-- 续租回执：重复续租返回原截止时间，不会再次延长（持久工作 3）。
CREATE TABLE renew_receipts (
    renew_id    TEXT    NOT NULL PRIMARY KEY,
    job_id      TEXT    NOT NULL,
    claim_epoch INTEGER NOT NULL,
    lease_until INTEGER NOT NULL
);
