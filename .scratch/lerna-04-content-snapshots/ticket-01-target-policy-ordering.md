# 04 F1 准确授权顺序澄清

2026-10-04；针对 fixed source `46d6ca26e4c2a4e9cf6db95e890b641f6bf2aa97` / delivery `72da5ee820a69695553ff208e10a8ba31804d0b8` 的 F1。只读必要决定，未测试；新实现尚未复核。

**采用 root 提出的 authorization-first 顺序。纠正上一架构报告 F1 的“在 request/record mismatch 分支之后核 policy.Ref”建议。** 那个次序虽然可保护正确 ref 的正文，却会让错误 policy 通过 integrity/not_found 观察资源存在性，不能作为完整关闭方案。

Get 共享 observe 的准确次序：

1. 保留初始当前 read/disclose 主体/用途检查并收集两项 policy 的完整 Ref。此处仍可按版本身份获取政策，不要直接要求 policy.Ref==request.Ref，否则会错误阻断已获准的声明错误查询。
2. 锁定该版本的实际 Record；在全部已发生的阻塞读取后重采可信 DB Now，保留当前期限、初始 admission 的 AcceptBefore 裁决。未作准确授权前不返回存在性、版本状态或正文；时限拒绝不得携带这些事实。
3. **record 非 nil：先要求每项 policy.Ref==record.Ref。** 不匹配即 rejected forbidden，且清除可继续披露的 record。全部匹配以后，再比较 record.Ref 与 request.Ref；不相等返回 rejected integrity。
4. **record 为 nil：要求每项 policy.Ref==request.Ref。** 全部匹配才允许 not_found；不匹配返回 forbidden。不存在准确记录时，不能拿相同 owner/id/version 的另一完整政策声明充当本次查询资格。
5. 其余当前保留期、direct-source、状态和正文门禁照旧；共享 observe 的读前/返回前两个调用均执行这个顺序。不改原 Command 历史、不产生新工作。

| 当前政策完整 Ref | 请求完整 Ref | 实际 Record.Ref | 允许的观察 |
| --- | --- | --- | --- |
| A | A | A | 继续当前状态/正文裁决 |
| A | B | A | integrity：主体确有实际 A 的观察资格，只是请求声明错误 |
| B | A 或 C | A | forbidden：不能先透露实际记录与请求不同 |
| A | A | 无 | not_found：本次准确查询有资格 |
| B | A | 无 | forbidden：不能借另一声明的政策观察缺失 |

A/B/C 在表中共享版本身份，但完整 hash/media_type/byte_length 声明不同；两项政策必须各自通过，不能只核其中 read 或 disclose。

该顺序同时满足已采用 contract-shape 的“有权限版本的错误声明→integrity”与“无权不泄露存在性”。必要测试保留正确 policy A/request B/actual A 的旧正常反例，并增加错误 policy 的 existing/absent 和真实回读后更换政策情形；上述只是待验证的公开行为，不是已执行结果。

这是同一 F1 准确资源授权的小范围澄清，不扩 inherited closure/生产 Grant、不新增 ADR/interface。原 fixed46 F1 继续 open，只有新固定源码与实际证据才能关闭。
