# 06 最终资格范围决定：不追加 child × secondary 故障组合

**决定：裁掉新 published-child→CopyToSecondary→offline 与另一次 ENOTEMPTY vertical，不将其列为票06必须出口。下一步是原五个父场景在当前共享 producer 上的固定回归，再完成最终受影响检查、独立审查与精确资源审计。** 此为 STATIC 范围裁决，不接受六AC、不执行 native、不修改源码或票状态。

## 固定来源

全文读取 `/tmp/lerna-06-after-late-put-remaining-static.md`（13604B，SHA256 `6523b0afe2d0082d8eef70e8e76b6db4ff94fba3e0a7c5ed861d230276225de6`）、原票06六AC及04 spec、两份实际 secondary 测试，并读取 policy-orphan 公开清理尾部与共享 producer delta 的相关部分。此次不是整个79561B测试文件的完整架构审查。

WT `/tmp/lerna-worktrees/content-snapshots-06` HEAD `c11fd2092875417d039914f8ecbd083a7a846d3a`；唯一未提交源码为 `conformance/component/content_process_recovery_test.go`，79561B、SHA256 `c9a098141d23cda9c40d210539384d7cfb17b0d6b4f0222c16d70577308ff7e5`。机器核 `/tmp/lerna-04-ticket06-execution/late-put-formatted-26-manifest.json` 全26项 bytes/hash均匹配，另外25项与c11f逐字节相同；manifest SHA256 `df707faeff172b1e817d124d8813da2ae9f1d1e3da90daf782263d3ddbbd47bc`。

独立逐字节比对 accepted05 `8db74e3` 与当前：`domain/content/secondary.go`、`domain/content/lifecycle.go`、`adapters/objectstore/local/erasure_linux.go`、`content_secondary_test.go`、`content_secondary_native_failure_test.go` 均相同。后两测试当前 SHA256 分别为 `8aeed0a0b5ab1413512d3277a466249637584753d294b52cd0a27cc721c474a2` 与 `6408b241d6082317e3a6749d6df0c0567028355b24587a322e29b97e33d811f7`。原05退出文档AC3明确记录真实独立副本、offline/ENOTEMPTY、原责任期限及恢复；本次读取源码与既有资格记录，没有重跑这些业务。

## 为什么不需要新组合

票06 AC3 的精确责任是“无法合法发布走05原对象清理/残留出口”，不是“所有05故障必须加一个06子进程前缀再验收”。原 policy-orphan 路径已经实际跨越这条接口：原SIGKILL→原Job/staging在当前Save撤回后失败→准确SealOrphan→真实staging/primary原holder责任→独立物理核验→allACK、重开Gone及固定failed进度；另一个原policy责任仍pending，不用voluntary seal伪造policy ACK。该测试约990–1148行的源码保留原ref/subject/purpose/deadline、原字节和公开观察，不伪造secondary。

05真实published copy/offline/ENOTEMPTY资格覆盖的是另一个合法分支：实际published才可CopyToSecondary，独立inode完整字节、原binding/CopyID/期限、实际关闭或真实unlink失败、残留负责方、重开推进原责任。当前06没有修改这些产品实现；新的 private pregate/PutResult 只观察原publication child的实际Put返回并加强协议字段校验，不进入secondary复制或清理路径。原child已关闭/Wait后再复制，不存在一个尚未验证的新在途共享writer机制要求额外产品装配。

因此两类既有资格可以按接口与准确源码分别支持对应AC，不能拼成“已执行child+secondary组合”的新事实。05原20秒caller、1分钟copy/seal/Work不冒称06原30/20边界；同时，也没有合同要求把05全部独立操作压入06同一个30秒caller。强加此前建议的新三holder页、offline、ENOTEMPTY组合会新增退出条件。

若最终审查发现具体cross-process holder绑定、接纳状态或原責任接法缺陷，再按真实反例单独收敛；目前没有这样的源码变化或证据，不能以可能性预建测试矩阵。

## 现在唯一下一资格及后续关闭路线

1. **执行已冻结的原五父场景当前源码回归**：durable-before-publication、precommit、postcommit-before-reply、current-Save-withdrawal/orphan、late-Finish，各原N/F/pairedR共15条。`/tmp/lerna-04-ticket06-late-put-static/source-commands.json` SHA256 `7f92447b27e57946e47da848cb9cfbcc9019a3d75df61bf55a9af6657a300d5f` 实际含5个对象、每个3条准确命令，非新增15个业务设计。新PutResult字段、gate枚举和release/closed解析确实改变了共同producer，旧c11f成功不能替它们验收。第一失败停止；只按root单独授予的唯一LOCAL执行，当前决定不启动它们。
2. 原caller30、child20、Claim5、I/O/Work4、Publish/management20、Tx3/statement2/lock1、Go/outer120与count1保持；初始Trust及deadline不续。latePut原已实际N/F/R成功是root已完整核定的该vertical证据，本次不重新声称执行；同源码不因形式再造重复probe。
3. 五场景资格完成后，按实际最终integration delta选择必需的正常/race/fixture/local/PG及仓库检查。现有两secondary测试可进入有限的最终Content affected-check选择或既有完整入口，保留它们自己的原期限与真实正常/恢复尾部；不复制新child测试，也不因源码未变反复追加专门回归。若最终源码同步改变上述产品，重新核对应影响，不能沿用字节等价结论。
4. 独立六AC映射/代码与规格审查/必要架构复核和修复，再做完整owned ledger资源审计、source/evidence精确pin、root整合验收。审计区分首实际Close、Wait/group absence、当前残留与业务allACK。历史8 killed scopes的firstClose UNKNOWN永久保留；新scope单独记账，不用后来的成功Close/空目录/进程消失补旧证据。故障期间需要保留的scope不清理。

全片04广告OFF、其他票与whole CI保持独立。票06关闭不新增票03容量/整个04退出/另一次secondary组合门槛；root最终接受、merge/push/准确CI仍按实际流程记录，本文不宣布任何一步已完成。
