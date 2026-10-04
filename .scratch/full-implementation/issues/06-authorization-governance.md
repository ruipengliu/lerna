# 06 authorization-governance

Status: partial
Implementer: governance_impl

授权、证据治理、扩展生命周期和冻结评测已有有界参考实现。领域合同见[治理说明](../../../internal/governance/README.md)，受信宿主见[adapter](../../../adapters/governance/README.md)。

## 完成依据

闭合 Grant issue/revoke/check/use/settle、完整父链/当前主体、原 IntentHash/Target/预算/once，可信原业务确认一次消费、EstimateAcceptance与有限离线GrantLease；在线delegation实际消费原once和有限USD上界，离线delegation在消费之前明确拒绝。late已知.5→.75/repeat按原签名源和原Use结算，unknown保留原预留，once不退款。

完整Evidence依赖/holder/current gate、持续Defect/change/ack、同库Result notice、101-holder分两页与失败回滚；BindingHead准确CAS、prepare/initialize/activate/close/reopen/dispose和独立旧批准回退；Evaluation固定plan/sample-arm/曝光/原分母/封存/取消及迟到资格失效。真实SQLite/PG合同与受信reference-rule-file两臂runner、独立目录/字面目标真值、实际Linux进程/SIGKILL原journal恢复均有证据。1001样本分页不代表1000次真实外部API。

Skill/AgentConfig完整小目录与真实Task选择已实现，普通Packet当前Source/holder/Caps/control和父Grant保持交集，不能授系统信任或成功。父选定Knowledge跨owner真实委派正反矩阵见[18](18-skill-agent-configuration.md)。受限WASI普通程序的Linux资格由工单13/15独立取证，不将其冒称任意治理扩展合格。静态EndpointChannel、设备有限Lease及Native Source已有独立运行证据。

广泛工单仍partial：远程正常完成的fee9正式叶已合454baaa；fresh SQLite父库／真实PG父库完整双方报告、三层Closure、原Task／Grant费用、必要proof字节、相关Job DONE及join／原cfg-token重开已通过。当前来源／holder和控制／close-before-latechild矩阵、复用Session其余资格仍待验证；最终固定版本审查与全检查待完成。公司身份/密钥发放轮换、真实跨治理Authority/高影响校准、live供应商最终账单/退款、开放自然语言/holdout质量、任意不可信扩展接管、自动发布、生产多AZ/容量与遥测另需资格验收。缺该资格时拒绝正式批准或保留原unknown；已消费once与费用收尾不能因新开始权撤回而抹掉。

真实治理PG测试读取HARNESS_GOVERNANCE_POSTGRES_DSN，宿主PG读取HARNESS_TEST_POSTGRES_DSN；skip不记通过。`go test -race ./internal/governance ./adapters/governance`是公开复现入口，准确已执行pin/命令/制品见实施覆盖报告，不能据此宣称当前新root已重跑。

统一阶段说明：当前CODE 58c9898af58babce7170cb37cbbdfc63d693af9e；本文实际通过只按所列原source／binary／selector及数据库制品限定。最新Saved十项、NoChild／Pause／现代Form／Closure后继资格已取得；最后完整检查／浏览器／全图审查尚未结束，旧失败／未跑记录不回填。
