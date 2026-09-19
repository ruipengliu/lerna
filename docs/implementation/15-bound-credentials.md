# 15：绑定目标使用加密凭证

CredentialBroker 将凭证管理和 Driver 使用分开：管理入口可保存和重包裹，受信 Driver 只得到绑定的 Use 入口。普通调用结果只有 accepted 位，不返回凭证正文、认证头或目标原始错误。15 票仍在验收中，最终状态以票据完成记录为准。

## 模块与边界

`credentials` 依赖消费方 Store、KeySource、Authority、Clock、Exit 接口，使用 Go AES-256-GCM。记录 AAD 固定引用、完整绑定（命名空间、主体、Driver、服务、账号、用途、位置）、格式、修订、期限和密钥版本。完整绑定既与记录比较，也在 Broker.Bind 时与出口实际 Target 比较。已知引用不能下载原始密钥。

`sqlitecredentials` 仅保存密文与元数据，单条修订 CAS，最多 64 条，每条正文最多 4096 字节，持久文档最多 16384 字节；文件 0600，WAL/FULL。`credentialauth` 通过现有 Harness policy + Grant 判定 credential.manage / credential.use，并核对实际主体和命名空间。管理者身份本身不隐含凭证使用权。

`filekeys` 的 Linux 参考实现要求独立的、当前用户拥有的 0700 目录和 0600 常规文件，拒绝符号链接及不安全权限；锁使用单独稳定 inode。密钥随机产生并以摘要标识，最多保留 8 个版本，每密钥默认最多 2^20 次加密，较低配置须持久一致。标准 96-bit nonce 使用独立持久计数，预留先于加密：文件 fsync、原子 rename、目录 fsync 后才交付 nonce，失败和进程退出不回收已用计数。其他平台明确拒绝该 Adapter，可替换 KeySource。

`credentialhttp` 为模拟目标提供固定服务原点和路径，账号/Driver/用途来自宿主绑定，不能由普通请求覆盖。默认要求 HTTPS；仅显式模拟配置可使用字面回环 IP 的 HTTP。关闭代理和重定向，使用有界超时，不返回响应正文，只有 204 映射为 accepted。参考目标约定认证头为 Bearer 加原始凭证的无填充 Base64；真实提供方的认证格式应由对应受信 Adapter 实现。

凭证请求不继承调用方 context 的 httptrace 等值，只传递取消和截止时间，避免认证头进入调用方观测。返回值、错误与目标回显边界有实际反例测试。该机制不防御已经控制宿主或受信 Driver 的攻击者，也不承诺 Go 运行时所有明文副本可被完全擦除。

`credentialexecution` 将现有 Execution.Start 接到 Broker.Use，以独立 Observer 确认实际效果；成功响应本身不被当作效果完成。验收经正式 SDK Invoke、签名单次授权、Core Worker 围栏、Execution、受控产物和 Outbox 完成同一任务。HTTP 模拟目标用自己的 SQLite 操作记录与计数状态判定结果，相同 operation_id 重放不会增加第二次效果。

## 管理入口

```sh
go build -o build/credentialctl ./cmd/credentialctl
./build/credentialctl init-key -key-dir /private/keys
./build/credentialctl put -config /private/credentials-config.json -ref account-api -expected 0 -expires 1900000000 < /private/secret-input
./build/credentialctl list -config /private/credentials-config.json
./build/credentialctl rotate-key -config /private/credentials-config.json -operation rotation-1
./build/credentialctl rotation-step -config /private/credentials-config.json -operation rotation-1
```

expires 必须是当前时间之后一年以内的 Unix 秒；示例值仅展示语法，应按实际运行时替换。secret-input 内容逐字节使用，不自动去掉末尾换行；从 stdin 读取，禁止通过参数传递秘密。生产使用可由受信交互或密码管理器提供 stdin，不需要保存这个临时输入文件。CLI 没有明文下载命令。stdin 同时受 4096 字节和主命令 10 秒截止时间约束；管道保持打开而不继续写入时返回固定错误，不写入凭证。

配置包含 AuthDB、TokenFile、CredentialDB、KeyDirectory、Resource 和 Binding，字段名与 Go 结构一致。TokenFile 是已有 Harness 管理凭据的受限文件；初始化身份、policy 和 Grant 沿 02 票管理入口完成。Binding 例：Namespace=local、Subject=alice、Driver=provider-v1、Service=https://api.example.test、Account=alice-account、Purpose=task、Location=local。配置不包含第三方 API 密钥或主密钥。CredentialDB 必须独立于 KeyDirectory。

16 票已将管理命令升级为持久操作，逐次 rotation-step 读取 Phase 直到 completed；历史 15 票报告对应其当时版本。当前入口详见 [16 票实施说明](16-credential-lifecycle.md)。轮换先准备并激活新密钥，旧记录仍可读取，然后按已知修订逐条 rewrap；每条原子提交，可依据记录修订继续。旧密钥不自动删除，不提前破坏未迁移记录及备份的可读性。当前最多八个保留版本，达到上限明确失败，维护者须按备份保留要求规划后续 KeySource 版本退役。

## 恢复与限制

nonce 高水位独立于凭证数据库，凭证数据库失败不回收 nonce。密钥目录本身必须作为不可回滚的当前权威材料管理，不能随旧数据库备份一起回滚。加密不证明整条有效旧记录没有被重放：恢复旧凭证数据库需受信恢复流程核对当前引用修订、失效和轮换状态，完成前不应启用新调用。在线防旧备份回滚设施不在本票的参考文件实现范围内。

文件访问控制保护数据库单独泄漏及非宿主主体访问；主密钥与密文同时泄漏或宿主被控制不在此保护范围。允许的明文生命周期限于受信 Driver 出口，第三方响应仅采用固定结构，不将目标错误正文写入任务、模型或观测。

## 验证入口

```sh
go run ./cmd/contractcheck -profile bound-credentials-v1 > build/bound-credentials-report.json
go test -race ./credentials ./adapters/credentials/filekeys ./adapters/credentials/sqlite ./profiles/credentialcheck ./cmd/credentialctl
make verify
```

命名 profile 当前 21 项：有状态目标、密文存储、重开、七个绑定维度、出口错配、缺失、过期、密钥缺失、密文篡改、引用移动、授权撤销、秘密回显、轮换、正式 SDK 执行、独立工作。额外包测试覆盖文件权限、并发 nonce 唯一与限额、真实子进程退出、部分 rewrap 中断并恢复、旧 SQLite 数据库恢复后的 nonce 唯一、CLI 管理与停滞管道超时、AAD 篡改、HTTP trace；不重复计入 profile 项数。不使用用户真实凭证、付费模型或外部业务服务。

## 最终验收

2026-09-11，完整 make verify 退出 0；运行候选 f5df39fa7623b9e75bd39e6c9a5977394089fb59、dirty=false。环境 linux/amd64，Go 1.26.1、protoc 36.1、Protobuf 生成器 v1.36.11；依赖精确版本及摘要见报告。原有 1407 项必需检查与本票 21 项全部通过，共 1428 项必需检查；含五项可选检查共 1433 项。

依赖验证、生成一致性、全包编译、go vet、完整 go test -mod=readonly -p 1 -race ./... 和 SDK 样例通过。定向包测试另覆盖真实子进程与恢复、权限和停滞输入，未重复计入 profile。Standards 与 Spec 独立审查均无剩余发现。

[本票报告](evidence/15-bound-credentials-report.json)、[验证阶段](evidence/15-verification-stages.json)、[完整日志](evidence/15-verification.log)、[审查修复及恢复测试](evidence/15-review-fixes-tests.log)、[stdin 修复前](evidence/15-stdin-red.log)、[stdin 修复后](evidence/15-stdin-green.log)、[HTTP trace 反例](evidence/15-trace-red.log)、[候选测试](evidence/15-candidate-tests.log)、[双轴审查](15-bound-credentials-review.md)、[证据摘要](evidence/15-evidence-sha256.json)。同目录 15-*-regression-report.json 保存本轮其余具名回归。

本票完成限定的凭证绑定与加密使用能力；批量轮换原操作进度、第三方续期核对及允许备份约束下的旧密钥退役由 16 票继续实现。
