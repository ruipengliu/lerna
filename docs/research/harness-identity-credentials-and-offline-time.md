# Harness 身份、凭据与离线时间的机制证据

核对日期：2026-09-09。范围：Go 可单机自托管实现所需的认证绑定、有限期签名授权、本地凭据保护与离线时间边界。本文是研究证据与候选实现建议，不是已批准的协议，也未实施安全功能。依据为官方标准和实现文档；动态 Go/gRPC 文档未作为项目依赖版本锁定依据。

## 已核实事实

### 节点 TLS / X.509 与 gRPC

Go 服务端设置 `ClientAuth: tls.RequireAndVerifyClientCert` 才要求有效客户端证书；`RequireAnyClientCert` 只要求提供证书。`ClientCAs` 指定客户端信任根，客户端的 `RootCAs` 与 `ServerName` 用于服务器证书验证；不应使用 `InsecureSkipVerify` 跳过正常验证。`VerifiedChains` 表示已验证链，不能把任意收到的证书当作可信身份。[Go tls.Config 与 ClientAuthType](https://pkg.go.dev/crypto/tls#Config)

`VerifyConnection` 在正常验证之后执行，也覆盖恢复的 TLS 连接；`VerifyPeerCertificate` 不在恢复连接上调用。Go `x509.Certificate.Verify` 明确不检查撤销，因此“链有效”不等于“当前仍被允许”。[Go tls.Config](https://pkg.go.dev/crypto/tls#Config)、[Go x509.Certificate.Verify](https://pkg.go.dev/crypto/x509#Certificate.Verify)

gRPC 区分通道凭据和逐调用凭据；TLS 通道可以与调用认证组合。Go `credentials.NewTLS` 接受 TLS 配置，`credentials.TLSInfo.State` 提供 TLS 连接状态。这些机制提供认证材料，不定义 Harness 的任务或通用能力权限。[gRPC Authentication](https://grpc.io/docs/guides/auth/)、[Go gRPC credentials](https://pkg.go.dev/google.golang.org/grpc/credentials#TLSInfo)

### 浏览器 UI 与 WebSocket

浏览器标准 `WebSocket(url, protocols)` 构造器没有任意请求头参数，不能照搬原生客户端设置 `Authorization` 的方式。握手使用 `credentials mode: include`，可携带符合浏览器 Cookie 策略的凭据；这不意味着跨站 Cookie 永远可用。[WHATWG WebSockets：握手及接口](https://websockets.spec.whatwg.org/#the-websocket-interface)

WebSocket 协议未规定独立的客户端认证机制，可以使用 HTTP Cookie 等机制。`Origin` 用于约束浏览器脚本来源；非浏览器客户端能伪造它，故不能把通过 Origin 检查当作用户或节点身份认证。[RFC 6455 §10.1、§10.2、§10.5](https://www.rfc-editor.org/rfc/rfc6455.html#section-10.1)

Bearer token 放进 URL 查询参数存在被日志记录的风险；RFC 6750 不推荐该传输方法。此规范针对 OAuth bearer token，但相同的 URL 暴露面也是 Harness 避免把长期会话或节点凭据放入 WebSocket URL 的依据。[RFC 6750 §2.3](https://www.rfc-editor.org/rfc/rfc6750.html#section-2.3)

### JWS / JWT 的授权边界

JWS 定义签名或 MAC 保护内容的格式；签名不提供载荷保密。JWT 定义声明表达：`exp` 后不可接受，`nbf` 前不可接受，`iat` 表示签发时间，`aud` 表示预期接收方。这些注册声明在基础规范中并非全部必填，应用必须制定自己的必填规则。[RFC 7515 §3](https://www.rfc-editor.org/rfc/rfc7515.html#section-3)、[RFC 7519 §4.1](https://www.rfc-editor.org/rfc/rfc7519.html#section-4.1)

JWT 最佳实践要求调用者限定算法集合、验证签名与实际算法一致，并验证签发者与密钥的绑定；存在多个接收方时必须检查 `aud`。不同种类 JWT 应使用可区分且互斥的验证规则，不能仅凭“某可信密钥验签成功”接受所有用途。[RFC 8725 §3.1、§3.8–3.12](https://www.rfc-editor.org/rfc/rfc8725.html#section-3.1)

Bearer 的性质是持有 token 即可使用，不要求证明拥有加密密钥。JWT 内写入 `sub` 或节点 ID 并不会改变这种性质；RFC 7800 的 `cnf` 也仍要求接收方执行实际的持钥证明，其确认方式由应用规定。[RFC 6750 §1.2](https://www.rfc-editor.org/rfc/rfc6750.html#section-1.2)、[RFC 7800 §3](https://www.rfc-editor.org/rfc/rfc7800.html#section-3)

证书绑定已有明确参考：RFC 8705 用 `cnf.x5t#S256` 表达客户端证书 DER 的 SHA-256 摘要，并要求资源服务将其与当前 mTLS 客户端证书匹配。这是 OAuth 的规范机制；借用到 Harness 自有授权格式时，应声明为项目协议约束，不能声称由普通 JWT 自动实现或已经完整兼容 OAuth。[RFC 8705 §3、§3.1](https://www.rfc-editor.org/rfc/rfc8705.html#section-3)

### 时间与重启

`time.Now()` 带有进程内单调读数；双方均保留该读数时，比较和相减使用单调时间。序列化、解析与 Unix 时间不会保留它；单调读数在当前进程外没有意义。部分系统休眠期间单调时钟会停止。因此，保存 `deadline` 或剩余 `Duration` 不能证明重启或休眠期间实际经过了多久。[Go time：Monotonic Clocks](https://pkg.go.dev/time#hdr-Monotonic_Clocks)

### 本地 AEAD 与主密钥

Go `cipher.AEAD` 的 `Seal` / `Open` 同时保护密文完整性并认证附加数据；附加数据不被加密。一般 AEAD 要求同一密钥的 nonce 唯一。`NewGCMWithRandomNonce` 在 Go 1.24.0 加入，自动生成、携带和读取 96 位 nonce；每个密钥不得加密超过 2³² 条消息。该 API 只接受由 `aes.NewCipher` 创建的分组密码。[Go cipher.AEAD](https://pkg.go.dev/crypto/cipher#AEAD)、[Go cipher.NewGCMWithRandomNonce](https://pkg.go.dev/crypto/cipher#NewGCMWithRandomNonce)

## 候选参考绑定（设计推论，待项目决策）

| 问题 | 简单、无需独立基础设施的候选 | 应用仍须承担 |
| --- | --- | --- |
| 运行节点身份 | Harness 管理私有信任根和节点证书；Go TLS + gRPC mTLS；从已验证证书的约定身份字段映射节点登记记录 | 首次配对可信引导、身份字段格式、证书签发/轮换/撤销、每次受保护操作的节点状态检查 |
| 浏览器用户身份 | 同源 HTTPS 登录与服务端会话，WebSocket 升级前验证会话 Cookie 和 Origin | 登录机制、Cookie 的 Secure/HttpOnly/SameSite 配置、CSRF、会话过期及登出后已建立连接的处理；浏览器兼容性仍需验证 |
| 有限期执行授权 | 非对称签名的短期 JWS/JWT；签发端保管私钥，运行节点只保存可信验证公钥；自有协议强制 issuer、audience、时间、类型与权限字段 | 选择并锁定 Go JOSE 库和算法；限制 token 大小；密钥 ID 只查受信任本地映射；禁止从 token 任意 URL 拉取信任材料 |
| 运行节点持有者绑定 | 自有授权 profile 要求 `cnf.x5t#S256` 与当前 mTLS 对端证书一致 | 证书轮换后的重新签发；转发链上的真实出示者定义；TLS 在代理终止时不能直接沿用端到端假设 |
| 第三方凭据保存 | 本地 AES-GCM 密文；主密钥由单独受限文件或操作系统凭据设施提供；数据库只保存密文和非秘密元数据 | 主密钥注入、备份/恢复、轮换、操作系统适配与进程权限；不得把这称为已完成安全隔离 |

以上是将已核实 API 与标准组合起来的推论，不是这些 API 提供的整套产品能力。尤其是“无需独立基础设施”只描述部署形态，不免除信任根和密钥管理责任。

授权 profile 还须明确任务、通用能力、资源范围、额度、授权 ID 与父授权关系。JWT/JWS 没有自动执行 Harness 委托链的规则；子授权不能扩大父授权的资源、动作、有效期或额度，必须由签发端及执行检查共同保证。`jti` 可作标识，但一次性消费或防重放仍需要接收方状态。这里是应用设计要求，不把签名格式当成授权策略引擎。

AEAD 主密钥与密文分开保存，可减少“仅数据库泄漏”暴露秘密的机会；若攻击者同时读取主密钥，或控制能解密的 Harness 进程，便不能承诺凭据仍保密。附加数据可绑定凭据 ID、所属主体和格式版本，但这些预期值须来自可信上下文；AEAD 也不阻止整个有效旧记录被回滚。这些是上述密码接口边界的设计推论，进程拆分、文件权限或本地加密本身不构成对恶意宿主的安全保证。

## 保守的离线到期规则（设计推论）

1. 在线领取授权时核验可信签发者与时间，建立本次进程内的截止点；截止点不超过 `exp` 与项目规定的最大离线窗口。在线状态更新若缩短权限，执行前采用更严格结果。
2. 执行前重新检查到期与当前可用的撤销状态。长连接或长任务不能因最初通过验证而永远保有执行权；已经发生的外部副作用也无法靠后来撤销自动收回。
3. 离线重启后，默认不恢复需要有限期授权的执行；时间来源重新可信并重新确认授权后才继续。休眠恢复、明显时钟回拨或时间不确定时采用同样规则。保存的墙上时间或剩余时长只作恢复记录，不作为期限仍有效的证明。
4. 如果产品选择离线重启仍可执行，必须另行定义可信时间/防回滚条件和愿意接受的风险。仅靠 Go 单调时间、签名的 `exp` 或本地文件不足以得出安全恢复结论。

完全离线时无法得知签发端刚发生的撤销。短有效期只能在验证时间可信的条件下限制暴露窗口；它不保证即时撤销。本地缓存拒绝列表也只反映最后一次收到的状态。这里没有声称保证离线撤销或离线时钟不可操纵。

## 未决事项与验证范围

- 尚未决定节点身份字段、首次信任建立、证书有效期/轮换及信任根恢复程序。
- 尚未决定浏览器登录方式、具体 WebSocket/JOSE 库、算法、token 必填 profile、委托及代理转发模型；这些选择不要求先部署独立身份服务。
- 尚未决定离线窗口、哪些动作允许离线、时间不确定后的用户恢复流程，以及正在进行的外部调用何时取消。
- 尚未选择各操作系统的主密钥保管方式；自动启动与人工解锁存在可用性取舍，需明确备份威胁模型。
- 本次只核对机制文档，未做代码审计、依赖漏洞审查、互操作测试、时钟回拨/休眠实验或安全认证。实施后应针对身份错绑、错误 audience/issuer/算法、过期与重启、证书轮换、重放和密文替换做行为验证。

## 补充：ES256 参考算法与编码边界

补充核对日期：2026-09-09。`ES256` 的标准定义是 ECDSA，曲线 P-256，摘要 SHA-256。其 JWS 签名字节必须是 `R || S`：R、S 各按大端无符号整数编码为恰好 32 字节，保留前导零，总长 64 字节；长度不符即验证失败。[RFC 7518 §3.4](https://www.rfc-editor.org/rfc/rfc7518.html#section-3.4)

普通 JWS Compact 的签名输入由受保护头和载荷的 base64url 编码以句点连接构成；最终签名字节再作 base64url 编码放入第三段。因此，ES256 不等于“对载荷 JSON 单独签名”。[RFC 7515 §5.1、§7.1](https://www.rfc-editor.org/rfc/rfc7515.html#section-5.1)

Go `ecdsa.SignASN1` 接收消息摘要并返回 ASN.1 编码签名，`ecdsa.VerifyASN1` 也接收 ASN.1 编码；该输出不能直接充当上述 64 字节 JWS 签名。曲线和散列一致并不表示序列化格式相同。[Go ecdsa.SignASN1 / VerifyASN1](https://pkg.go.dev/crypto/ecdsa#SignASN1)、[RFC 7518 §3.4](https://www.rfc-editor.org/rfc/rfc7518.html#section-3.4)

设计建议：可把 ES256 作为 Harness 非对称短期授权的具体参考算法，并在验证配置中固定允许算法及 P-256 密钥类型。由符合 JOSE 规范的实现处理签名输入、base64url 与 R/S 编码，不在业务层手写拼接或 ASN.1 转换；再独立执行前文的声明与权限检查。这是降低实现失误的项目建议，不是选定第三方 Go 库或版本，也不替代实现后的互操作验证。
