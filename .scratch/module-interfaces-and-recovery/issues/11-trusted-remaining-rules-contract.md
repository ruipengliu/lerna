# 11: 模型、模拟目标与查询完成受信规则迁移

**What to build:** 所有现有目标证据均通过同一受信 Interface 解释，原历史仍按原规则读取与恢复。

**Blocked by:** 10（FILE 证据与原版本查询使用受信规则 Adapter）.

**Status:** resolved

- [x] 迁移 simulator、model 及剩余 QUERY 的固定证据规则；Execution Manager 保留冲突、效果历史与收尾裁决。
- [x] 删除已迁移协议的过渡分派、旧解析实现和无调用 helper；不存在两套可选择的同版本解释路径。
- [x] 模型回报、模拟写入、独立查询、弱否定证明、查询回执丢失和迟到效果通过公共流程验收。
- [x] 原规则标识、结果、迟到可能性、等待原因及目标/模型/账单计数保持；查询终局不代替原动作终局。
- [x] 所有原记录，包括完成、停用和被替代历史，按原支持版本纯核验；未知版本恢复前拒绝且 READY 工作不领取。
- [x] 不改变 schema、格式、默认 prompt、费用解释或当前 credential-unavailable 行为；设计和相关普通与故障检查通过。

## Comments

2026-10-07: Claimed after ticket 10 resolved, on codex/interfaces-11.


## Answer

2026-10-07: 原 simulator 写入、独立 QUERY 和 model 终局解释已迁入 `infra/rules.Simulator`、`infra/rules.Model`，`rules.Fixed` 成为全部原 API、FILE、模拟目标、模型和查询的编译固定清单。规则与解释前资格核验拒绝未知原版本，没有插件注册或未知协议回退。执行管理中对应的过渡分派、旧解析实现和无调用 helper 已删除；两个参考 QUERY 只共享已核验后主体事实的原样映射，各自保留原字段白名单、目标／查询／主体绑定和 Rule 标识。执行管理继续直接重放已保存解释及回执，持有全部发送的历史投影、冲突、收尾、查询关联和结算责任。

`reference-target-v1`、`reference-model-v1`、`reference-query-v1`、`reference-query-subject-v1` 的原结果、迟到可能性及原因保持；查询自身的终局不代替原主体终局。模型原输出回放、拒绝／不完整／无效输出、未知及迟到费用、模拟写入和矛盾字段、独立查询、弱否定证明、查询回执丢失、迟到效果、全部物理发送和固定关闭 Result 通过既有公共命令、原负责方查询与独立目标／模型／账单计数验收。所有原 Operation 恢复前先核验受信规则资格，完整固定声明、描述及原关系继续由原编译器独立纯核验；完成、停用和被替代模型历史不跳过版本检查。资格核验不调用 Compile、不读取正文或解析凭据、不续期或发送。未知版本在另一 READY 工作领取前拒绝，原回执、费用和目标计数保持。schema、持久格式、默认 prompt、费用解释和当前 credential-unavailable 行为未改。

执行管理设计先于代码更新。基线通过；新增 simulator 与已完成 model 的公开配置检查分别先复现缺失 rules 被接受的 red，再验证 nil／typed nil 明确拒绝、原 Operation 和独立计数不推进，固定原规则可读取历史。主实现 `fe410b6` 后同步集成 `2a68bcb`，最终验证源为 `2d66ff38478fdbf457282a2aa13cb11e6a5d8494`，树 `546cfe39062510056dd238f03776511ebf1b947b`。同源公共普通 race 切片通过（54.390s），原声明／绑定、未知版本、完成／停用／被替代历史的 fault+race 切片通过（81.873s）；`make check-code CHECK_PACKAGES='./core/ledger ./infra/rules ./cmd/assembly'` 的格式、普通／fault 双 lint（0 issues）、规则与 scoped race，以及 `make check-docs` 均通过。准确命令、源码和 baseline/red/green 记录见 `/tmp/lerna-module-interfaces-implementation/ticket-11.md` 及 `ticket-11-merged-*.log`。

本次合并树与该已测树完全一致，复用精确同源检查，只额外记录工单 11 的验收状态和 Answer。没有启动完整存储／故障矩阵；最终评审后由集成候选的 `make check` 统一验收。父 spec 与三个原有用户编辑保持不变。
