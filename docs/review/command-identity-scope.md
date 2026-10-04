# 命令身份必须在 SDK 与核心之间保持完整

建议在冻结 SDK 协议前统一命令身份。当前网关文档遗漏了 issuer_id，却声称与核心契约一致；这是协议定义的直接矛盾。优先级中，必须在多端点和多扩展接入前解决。

---

定位：[核心契约第 3.1 节](../architecture/core/contracts/README.md#31-命令回执查询通知与错误)第 187 行把幂等范围定义为 `(user_id, issuer_id, target_domain_id, command_id)`；[持久工作第 2.1 节](../architecture/core/durable/README.md#21-命令回执)明确把调用者命名空间映射到 issuer_id；[网关与 SDK 第 2 节](../architecture/platform/gateway/README.md#2-对象与状态)第 54 行却使用 `用户 + 接纳责任域 + command_id`。该文的查询接口也没有明确携带原 issuer_id。

---

反例：同一用户的两个合法调用者 A、B，对同一裁决域使用相同的 command_id。按核心契约，它们是两个独立命令；若 SDK 的待发记录、查询键或退役记录按网关文档实现，则两个命令可能互相覆盖，或者 B 查询到 A 的回执。全局随机 UUID 只能降低偶然碰撞概率，无法解决显式重用、导入记录或测试向量的协议含义。这里不推断网关已经实现了错误去重，仓库目前只有互相冲突的设计定义。

---

修改方案：在核心契约定义唯一的 CommandIdentity 类型，提交、查询、回执、交接、SDK 持久记录和退役记录全部引用它。身份认证得到当前调用主体，核心验证该主体是否有权提交或查询这个 issuer 命名空间；不得只相信正文自报的 issuer_id。SDK 刷新令牌、重连、切换网关或进程重启，都保持原 issuer_id。密钥轮换改变认证证明，不改变原命令的身份。

跨域交接由源域使用固定的服务命名空间创建目标命令；明确保存源命令与目标命令之间的映射。源客户端原 issuer 作为因果来源保留，不冒充目标域眼中的调用主体。命名空间退役后，迟到的旧命令必须被拒绝；不能给新的安装实例复用已经退役的 issuer，再让同一 command_id 获得新解释。

---

改动落点是 gateway 第 2、3、4.1 节与 contracts 的公共结构定义。数据库唯一键、SDK 主键和查询测试向量采用同一个类型，避免每篇文档重新列字段。验收覆盖四组组合：相同 issuer/相同 id/相同正文只决定一次；相同 issuer/相同 id/不同正文冲突；不同 issuer/相同 id 在各自获准范围内独立；轮换身份凭据后仍能找回原决定，冒用其他 issuer 被拒绝。

---

一手依据：AWS 的幂等设计以调用者与调用者提供的请求标识共同识别重复，并把意图记录和业务修改放在同一原子事务中。[Amazon Builders’ Library：Making retries safe with idempotent APIs](https://aws.amazon.com/builders-library/making-retries-safe-with-idempotent-APIs/)（2026-10-04 核查）。它支持“明确调用者范围”的理由；Lerna 最终采用哪些身份字段，仍由本项目契约决定。
