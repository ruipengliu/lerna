# 旧 holder 升级的三条故障恢复资格

各原始日志和 SHA 来源见 [provenance](provenance.json)。原 caller20s、Go30s、wrapper120s、编译15s裁至caller、旧producer5s／supervisor6s及join3s保持。每轮为新准确自有scope，所有旧逻辑Close与actualWait／group absence先于当前消费者。历史unknown scope不进入这些新资格。

| 真实场景 | 普通 | race | 准确 partial pin |
| --- | --- | --- | --- |
| 原Read后合法政策收紧，全记录旧前态拒绝，同原资格重开恢复 | [2.273s](legacy-whole-record-race-first-run.log) | [3.852s](legacy-whole-record-race-race.log) | 2402fd79d5093e0a29605f4e28f2da0c1cac8cac |
| 原Store确认commit后丢一次返回，同原资格重开恢复及全部原attempt清理 | [2.316s](legacy-committed-reply-loss-first-run.log) | [3.832s](legacy-committed-reply-loss-race.log) | ca9ac8110c091bacfdda492d64185c9e496a0582 |
| 同最初700ms资格，V2正常；V1真实CAS及attempt写后跨原截止，整Tx回滚 | [2.702s](legacy-initial-cutoff-first-run.log) | [4.133s](legacy-initial-cutoff-race.log) | 331154b132c5fd631fb8f663e05a12642cd014cd |

记录竞争通过真实Management政策入口发生，不改私有数据库tuple。返回丢失仅是已确认提交后的机械返回故障，不是PG commit_unknown。截止case只初次缩短原资格，不在失败后续期；绑定回滚有公开unavailable／Seal拒绝及独立字节证据，attempt持久回滚关联同事务原子性属于源码证据，不声称公开私表计数。原字节、固定回执、出版历史及独立V2均验证保持。

这些是现有协议的直接资格，未引入产品补丁或伪造业务red。整页责任CAS、完整祖先／AdmissionTarget清理因果、最终受影响检查及独立审查仍待；七AC未接受，产品尚未正式合入集成。
