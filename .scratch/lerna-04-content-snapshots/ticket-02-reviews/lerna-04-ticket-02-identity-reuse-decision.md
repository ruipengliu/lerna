# 04 票02：两个身份计算的严格相等快速路径

2026-10-04；授权 Astra/high 必要窄决定。实际读 `/tmp/lerna-worktrees/content-snapshots-02` 的 `adapters/postgres/content/{policy,facts}.go`、`domain/content/identity.go`、1.2 SubjectBinding/DelegatedSubject 类型及两个指定日志。上述三个实现对象相对固定 `75202ee0077b5bf12b432b5017ad97a1dfcab681` 无差异；其他 WIP 不作固定交付资格。未运行 native/DB/build/环境命令、未改源码、未读另一轴。

**采用两个局部相等快速路径，LockVersion 用“完全正常才跳过，否则走原验证”的最小调整。** 不增加 helper 框架、port、全局或跨调用缓存，不改变机器合同和持久身份算法。

## 1. CheckPolicy：完整 SubjectBinding 相等才复用

当前位置：`adapters/postgres/content/policy.go:33`。函数在 SQL 前已经 `subjectKey(subject)`，由 `v.Encode` 严格校验并编码完整主体，然后 SHA256。实际主体形状是 TenantID、SubjectID、完整有序 DelegationChain，每层又有 TenantID/SubjectID；不能缩成 subject_id 或链长度比较。

在原政策行解码、tuple/purpose/ValidUntil 条件之后，令 match 初始为已验证 key；只有 `!reflect.DeepEqual(policy.Subject, subject)` 才执行原 `subjectKey(policy.Subject)`，保留原错误传播以及 `match != key` 的拒绝路径。相等分支等价于对同一完整合法值重复运行确定性 Encode/hash，省掉的是这次重复纯计算。nil 与空 slice 的 DeepEqual 不相等，只会回退原路径，不可为了更多命中把它们强行视作相等；不相等也不能直接 forbidden，仍保留原严格验证及编码/散列比较语义。

`core.SQL` 的 owner/token 校验、查询条件、FOR SHARE、锁后 core.Now、政策解码及元数据核对、五动作 switch 全部保留。域内准确 fullRef/F1 授权顺序不变；相等主体不能证明准确 ContentRef，不能跳过任何资源或动作授权。此处不存在将已验证 key 当下一次调用授权的缓存。

## 2. LockVersion：仅完整正常身份跳过第二次计算

当前位置：`adapters/postgres/content/facts.go:84`；原 `Record.ValidateIdentity` 位于 `domain/content/identity.go:43`。首次 `VersionIdentity(ref)` 已严格验证完整请求 ContentRef，返回准确 id 和 objectKey；将原被丢弃的第二返回值保存为 `expectedObjectKey`，避免与后续 advisory-lock JSON key 变量混淆。

保留现有 SQL、advisory lock、FOR UPDATE、json.Unmarshal，以及 record 对 SQL列的 Owner/content_id/version/ObjectID/ObjectKey/TupleDigest/Publication/Revision 一致性检查。之后采用：

```go
if record.Ref != ref || record.ObjectID != id || record.ObjectKey != expectedObjectKey {
    if err = record.ValidateIdentity(); err != nil {
        return nil, err
    }
}
```

因此只有 record.Ref 与已严格验证 ref **完整 Go 值相等**且两个身份值也匹配时，省去第二次 schema 验证、JSON 编码与 SHA。任何不相等/损坏情况仍经过原 ValidateIdentity，保留精确原校验错误及 `Content version identity mismatch` 错误构造，不复制一个新错误常量或改变分类。

特别是同 tuple 但 hash/media_type/byte_length 不同的请求，不能在 adapter 新增一刀切拒绝：原 record 可能本身合法，后续域层仍须保持已授权 mismatch→integrity 等既定顺序。完整不相等走原验证恰好保留这一语义。SaveVersion 等其他消费者继续调用原 ValidateIdentity，不把它泛化成“预验证”公开接口。

## 3. 证据及验证限度

已读 `closure-port-timings-race.log`：原完整用例在 i61 Step、60.05s 超时，native status=1、group_absent=True；不是 race green，也不是 unknown 外层退出。CheckPolicy 的 Put/Step 共5735/11383次，约7.789/15.191s；LockVersion 2015/3815次，约3.699/6.839s；ScheduleRetention 62次约12.521s 包含其子调用，不能相加冒总耗时。

已读 `closure-identity-cumulative.log`：来自旧同一60.05s采样的 subjectKey 累计CPU约2.14s、ValidateIdentity约1.08s；包含编码、验证和调用链重叠，不能倍数推算收益，更不保证该小改足够过原60秒。profile 工具自身 status=0 只证明该工具结束。

sole owner 需在现有完整 normal/race/有限退出覆盖下验证：正常完整相等命中；委派链不同/畸形主体保持原错误或拒绝；同 tuple 不同 fullRef 仍保留域层授权与 integrity 顺序；行身份列/正文不一致仍拒绝，record身份自身错误仍走原验证。可用当前准确已有用例覆盖并补实际缺口，不为两个布尔短路新造庞大测试框架。机械损坏夹具只证明注入情况，不冒物理存储损坏证据。

原 SQL次数、锁后时间、动作调用、所有预算、64/65完整集合、原 deadline 与当前最终门不动。新源及真实完整有限运行未通过前，仍不得称性能阻塞解决或票02验收。
