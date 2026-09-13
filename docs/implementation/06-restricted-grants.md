# 06：签发、派生和撤销受限授权

对应 [06 票](../../.scratch/harness-implementation/issues/06-grants.md)。本票扩展本地授权权威，交付固定签名授权、有限委派及预留、祖先撤销与接收方应用核对。实际节点登记、远程同步和业务执行仍按后续票交付。

## 使用入口

```sh
go run ./cmd/contractcheck -profile restricted-grants-v1
make verify
```

在已有 authorization.Service 上组装 GrantAuthority，复用同一 AuthorizationStore。签名器通过 GrantCrypto 接口注入；默认 josegrant Adapter 使用由宿主提供的 P-256 私钥及固定 kid 公钥映射；缺少签发 kid、空映射、无效点或公私钥不匹配时拒绝启用，组装时复制密钥以固定本实例的配置。LoadPrivateKey 可读取受限的 PKCS8 PEM 文件，拒绝符号链接、非普通文件、过宽权限和超大输入；私钥不保存到授权数据库或业务消息。

```go
key, err := josegrant.LoadPrivateKey(keyPath)
// 检查 err；trustedKeys 来自受信宿主配置，不能从请求取值。
crypto, err := josegrant.New("signer-1", key, trustedKeys)
// 检查 err。
grants, err := service.SignedGrants(authorization.GrantConfig{
    Issuer: "local-authority", MaxTTL: time.Hour,
    IOTimeout: time.Second, ClockSkew: 0,
    MaxDepth: 4, MaxRecords: 64,
}, crypto)
// 检查 err。
client := sdk.NewAuthorizationClient(
    authlocal.BindGrants(service, grants, credential), "local")
operationID, err := client.NewOperation(ctx)
// 检查 err；revision 来自当前策略视图。
receipt, err := client.MutateGrant(ctx, &harnessv1.GrantMutation{
    OperationId: operationID, ExpectedRevision: revision,
    Kind: "ISSUE", Spec: spec,
})
// 提交未知时保留原请求，用 LookupGrantOperation 核对原操作。
```

Spec 指定主体、目标接收方 audience、代理出示者 presenter、已验证证书的 SHA-256 base64url 摘要、资源/动作/用途/位置范围、nbf/exp、委派深度、有限 units 和 continuous/single 模式。single 要求 units=1、无继续委派，且绑定原业务 operation 与语义 SHA-256；管理签发操作与原业务操作是两个不同意图。

MutateGrant 的 DERIVE 指定父 grant_id 和 expected_grant_revision，REVOKE 指定撤销目标及其修订并不携带 Spec。GetSignedGrant 返回当前记录、累计分配和通知应用情况；LookupGrantOperation 返回原不可变回执。旧 Bind 只提供旧授权接口，对新增操作返回 UNSUPPORTED。协议仍位于任务协作的 AuthorizationService，不增加新的协议领域或远程服务。

## 签名核验与当前资格

参考 Adapter 固定 compact JWS、ES256、`typ=harness-grant+jwt;v=1` 和显式 kid，只接受这三个保护头。未知头、重复 JSON 键、非规范 base64url、算法混用、未知密钥或错误类型均拒绝。密钥映射从受信配置查找，不读取材料中的远程地址。实现采用锁定的 go-jose v4.1.5；其 API 要求显式限定解析允许的签名算法，参考 [go-jose 文档](https://pkg.go.dev/github.com/go-jose/go-jose/v4)及[项目源码](https://github.com/go-jose/go-jose)。

签名包含 iss/sub/aud/iat/nbf/exp、cnf.x5t#S256、命名空间、授权引用、签发修订、父引用及具名 harness Spec。Verify 先核验编码/签名，再比较受信 GrantPresentation 的主体、接收方、出示者、证书摘要及操作绑定，最后查当前本地权威记录、主体登记、完整祖先链与当前策略。签名不能替代实时授权，也不因旧材料仍可验签而忽略父级撤销。

GrantPresentation 是已认证宿主或将来 mTLS Adapter 提供的内部上下文，不由普通请求填充。profile 生成并验证证书链，从实际证书 DER 计算摘要，同时验证错误出示者和跨接收方重放拒绝；该证据不代表已有生产 mTLS 服务。

时限配置有界：TTL 为 1s–1h，I/O 为 1ms–1s，允许的签发时间偏差为 0–1min。nbf 与 exp 不因偏差延长；回拨由共享时间水位拒绝。本票每次核验均依赖本地权威状态，不实现依赖远端授权的离线资格。Store 与密码适配器须遵守 context；签名超时后不交付材料。

## 委派与额度

资源使用既有受信精确引用、集合或登记树选择器；比较不能通过自报归属或当前树展开巧合扩大未来权限。动作、用途、位置、期限和深度逐层收缩。默认只允许派生给同一主体/接收方/出示者/证书；跨目标委派必须在父 Spec.delegate_targets 中明确列出，子目标集合也必须被所有祖先允许。该列表不是节点登记或认证依据。

每个 Grant.units 是一个有限池。DERIVE 在同次提交中增加父记录 allocated，并给子记录分配独立池；父级自用通过 ReserveUse 从同一余额分配。多级分配在父级已留给子级的范围内进行，不再从祖先重复扣减，也不会重置父级余额。root 由有权签发者在范围内建立有限池，不声称跨所有根授权存在未定义的全局额度。

Verify 是纯授权判定，不消费额度。开始业务工作前，受信处理方调用 ReserveUse，持久绑定命名空间、Grant、接收方、出示者、业务操作、语义摘要、动作摘要及数量，取得 UsePermit 后才交付消费方。同一请求恢复原预留，换操作、语义、Grant 或接收方不能复用原占用。

UsePermit 是分配回执，不是业务已执行证明。消费方必须在自己的业务事务中登记许可与原操作；开始前仍复核当前资格。profile 用独立 SQLite 消费方在同次事务中写许可和一次模拟效果，重开后重放不增加效果。本票未将该夹具称为生产 ExecutionStore，也未实现真实 API、GUI、记忆读写消费。原操作恢复通过 LookupUse 取得已有分配，不授权新的启动。未知交付或消费、过期及撤销均不自动退款。

## 原子提交与恢复

签发分成固定内容准备、事务外签名、最终权威提交。最终提交重新核验当前管理资格、策略修订、父授权与余额；授权记录、预留、原回执、材料及待通知状态共用已有 SQLite CAS 边界。并发修改使旧准备失效，签名成功但提交冲突/未知时返回错误，不把未确认材料交给调用方。

原操作先核对语义再进行新操作的版本比较；管理、任务控制和新增签发不能重用彼此的身份。签名材料与 Grant 操作回执独立保留，不随旧管理短期回执清理丢失。重开原文件仍返回原已提交材料，不重新签发或追加预留。原操作窗口关闭只阻止新的首次提交，不复活旧操作。

Format=1 增加可选 Signed 分区，旧数据缺少字段时仍按旧含义读取。LocalGrant 保留 continuous 语义，不隐式升级成签名授权；签名授权也不绕过既有任务控制和 RunStore 预算。无须删除数据库；旧二进制写入新增状态的降级不受支持。

每个权威最多 1–512 个 Grant、同等数量的自用预留；签发/撤销回执最多 2×MaxRecords，预留每个 Grant 一次撤销空间。新的操作不能再次撤销已撤销记录，重放原撤销仍返回原回执。链深度最多 16，delegate_targets 最多 16，单份材料最多 32768 字节，管理输入最多 16384 字节，units 最大 1000000。共享快照仍受原 16 MiB 上限约束。容量耗尽明确不可用，本票不自动归档或回收未知占用。

## 撤销及应用确认

Revoke 修改权威记录及修订，并为受影响派生树的接收方记录 pending 通知。Get 分别返回自身 revoked、当前 revoked_ancestors 及各接收方的 applied；父级撤销不需要改写全部叶子签名。

ConfirmApplied 是受信接收适配器接口。接收端先将权威状态和自身已应用修订原子保存，随后确认指定 Grant/修订；接口核验原接收方及出示关系，旧修订或错误来源不能标记已应用，重放原确认幂等。应用确认不更改权威修订，不以消息发送或持久接收回执代替它。夹具保存独立的接收方应用事实并验证确认丢失后的恢复；没有构建远端传输或全量同步。

撤销仅阻止新受限工作，不能把在途动作写成已取消。后续执行/任务处理器继续保存已经发生的事实并按原收尾契约核对。

## 验证范围

restricted-grants-v1 当前包含 25 项契约检查和 5 项真实 SQLite 子进程检查。覆盖 SDK 重放、管理权限、证书绑定、独立 ECDSA/JOSE 编码、严格头/声明、时间及策略收紧、委派收缩、多级与并发额度、单次绑定、父级撤销、应用确认、签名冲突/不可用、未知回包、旧本地授权、跨域身份与满额撤销。

子进程在 signed-before-commit、grant-committed、allocation-committed、revoke-committed、application-committed 退出，每次有 10 秒上限。独立消费方和应用事实提供恢复核对依据；不以控制器退出推断远端消费已撤销。

委派负例使用宽策略和较窄父授权，独立验证动作、用途、位置、期限、代理及资源的父级限制；签名期间父撤销和两个派生请求竞争均通过真实 SQLite 独立句柄验证。

旧授权编码夹具由 `37a51da` 生成，SHA-256 为 `38719de0f52a20c5f5f6d8bbc8055ae854818215e61defebbb73587b44ce927f`。升级检查核对旧主体、旧授权和原管理回执，新增签名授权后重开原 SQLite 仍可核对；来源与公开测试身份见 [夹具说明](../../profiles/grants/testdata/README.md)。

## 完成与验证记录（2026-09-10）

实现提交 `76970d6`，审查修复提交 `48d51a1`。最终 make verify 全部通过：依赖校验、生成一致性、编译、go vet、全量 race 测试、SDK 样例和所有适用 profile。

- [受限授权报告](evidence/06-restricted-grants-report.json)：30 项必需检查，包含 25 项契约和 5 项真实子进程恢复。
- [验证阶段](evidence/06-verification-stages.json)：全部阶段 passed。
- 01–05 回归：[SDK 32 项](evidence/06-sdk-regression-report.json)、[授权 30 项](evidence/06-auth-regression-report.json)、[任务 13 项](evidence/06-tasks-regression-report.json)、[Worker 26 项](evidence/06-worker-regression-report.json)、[控制 31 项](evidence/06-control-regression-report.json)。旧 profile 的限制描述仅对应各自测试范围，新增签名授权能力由本票独立报告提供证据。
- [双轴审查](06-restricted-grants-review.md)：Standards 1 项 P3、Spec 2 项 P2，均修复并经原审查者复核关闭。

报告对应修订 `48d51a1f5a09ecc79fcb23163feb599ad1a2f0d8`，构建 dirty=false，Go 1.26.1、linux/amd64、SQLite 3.53.4、go-jose v4.1.5。后续完成记录提交仅包含文档和报告，历史验收文件不改写。本票证据不替代实际端云认证、业务消费或完整系统验收。
