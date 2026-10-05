# 04票06：原live worker在真实fence之后调用Put——下一静态决定

**采用唯一下一纵向场景：原Claim及attempt提交 → 原local.Put调用前私有gate → 父原句柄SealOrphan/全部真实holder ACK → 同原child调用真实Put并观察ErrBodySealed → 原公开失败/Gone与恢复尾。** 一个normal_release和一个fence_before_late_put，分别真实执行后再配对race。仅允许所述test-consumer私有门/返回witness，不增产品端口、迁移、框架或下一secondary矩阵；本文不授权native或修改源码。

## 固定资格

完整读 `/tmp/lerna-04-ticket06-late-put-static/plan.md` SHA `2d4f116870c82563826f48bdd4462711073c941e7efa8444f53b1abcdf53656a`；actual-existing-port-pins SHA `a7aacca06bd6d7c9c1bc2235c2f76572f0577e71d9d206d361a1dd2822b81696`；commands SHA `50da846992b9bd31d6e7c89f4de72f87176680a76c986c768fcede86e6072c9c`。逐一核对13个列明文件长度/SHA全部匹配。

当前 `c11fd2092875417d039914f8ecbd083a7a846d3a`；原26manifest `bbc855b582b9ee1ff30e7fca018f517b345abaec6befc83d6a71ccbcc060cca4`。实际consumer60913B/SHA `5cb4745322f488ef48d196ed617e7ebccecaf77793d4e35e723d3b439a0cef41`。root已核firstorder N/F/R，仅承接其准确历史资格；本轮不声称重跑。计划的later secondary/offline/ENOTEMPTY部分只作为边界说明，不在本次采用范围。

## 1. 为什么该witness必要且足够窄

现 `contentProcessObjects.Put` 先调用真实local.Put再处理durable gate；不能借该gate证明“尚未调用Put”。Service final根据BodySeal优先收口forbidden，可能吞入底层ioErr后返回worked=true/err=nil，因此closed帧/failed progress不能证明真实物理封存拒绝。

新增tag `claimed_before_actual_put` 的独立前置分支即可：真实Service原领取/保存attempt事务返回成功后才进入Objects.Put，所以不需假Record或新Commit marker。分支先核对cfg原fullRef对应ObjectID/key、Claim.Object完整owner/kind/id、原phase=publish/epoch及 `attempt == 原key+原epoch的现有格式`，另核原hash、length6。仅从实际参数和捕获Claim读，不写回Claim/Job或替换对象句柄。

Emit原tuple/lease/key/attempt、Receive准确同scenario/generation/release均使用传入的原I/Octx。只有正确release后才调用 `o.Store.Put` **一次**，全部参数/原ctx/原Store保持；无release、错误release或超时不能继续调用。

实际返回后立即在同event pipe发一个 `original_put_returned`：PutResult闭集 `succeeded/body_sealed/other_failure`，分别来自nil、`errors.Is(actualErr,content.ErrBodySealed)`、其它真实错误。准确tuple与pregate一致。无postcall hold/新pipe/新背景goroutine。成功emit返回原error；emit失败返回Join(actualErr,emitErr)，不丢第一因果。父先消费该Event，再调用现closed Reply/Wait/Stop helper，不能反过来让事件管道阻塞。

## 2. 必要的协议与失败限定

- 只有新tag发新事件。旧durable/precommit/confirmedCommit分支原顺序不动；新增PutResult建议omitempty，使旧stage空值不增加其序列化字段。新stage严格核enum，其它stage拒意外非空PutResult；不扩一般CauseClass列表或原36上界、不输出原error文本/正文/DSN。
- `body_sealed`只证明实际错误链包含封存拒绝，**不消除joined取消/IO/关闭错误**。保留已有有限cause分类；出现deadline/cancel/nativeIO/Close未知即本场景未通过，原错误与资源责任保留。纯ErrBodySealed在旧通用分类中显示unknown_cause/type可以接受为诊断局限，因为专用enum来自实际errors.Is；不能把未知其它error改填body_sealed。完整成功仍须实际first Store/Object CloseACK、Wait与pipe Stop。
- Q仍为父在start helper之前的单调t0+4秒：原Trust与gate真实LeaseUntil均覆盖Q，release、实际Put Event、firstClose/Wait/Stop都必须在Q内。Q不是修改后的ctx或授权，不证明包装Receive.nil；这次额外的实际Put返回事件才证明所选物理分支。任一首次失败立即保真STOP，不删除Q/重试到green。

## 3. 下一完整出口

同一原caller30、固定原Trust及initial SealDeadline≤initialNow+20/Trust，Claim5/Work4/I/O4、child20/Publish20、management20/PageSize2、Tx3/stmt2/lock1、原max3、Go/outer120/count1/p1/readonly全保持。

pregate先公开原preparing/固定receipt，并独立证原key和声明attempt缺失、原staging真实存在。**normal**立即release，观察succeeded，真实全6bytes/hash/length；实际child关闭join后才Reopen，公开Published、固定receipt+published progress和原完整bytes。先有此对照，不能仅运行拒绝分支。

**fence**保持同原live child、Claim/FD/实际保存basis；父原句柄SealOrphan，原PolicyChangeKey为空，只有实际staging/primary两holder。最多四次Lifecycle.Step是有限上界，不是固定调用数oracle；逐实际holder_id观察、独立确认fence/erase/原identity与无residual/cursor。此时primary从未安装正文：不能套用firstorder“另一primary仍有6字节”的断言。primary先ACK时staging仍Present；staging先ACK时primary公开仍pending且正文原本缺失，直到真实物理fence资格完成。

两holder真正ACK及CleanupComplete后才release。必须观察原tuple的actual Put `body_sealed`；EOF、deadline、other_failure或仅Step(nil)不算。local.Put在原key flock下检查真实sealed标记，不能绕成测试返回固定错误。child结束后独立key/attempt/staging仍缺失，再Reopen核同seal/deadline/fullbinding/allACK，原receipt不变、progress failed；原body权限拒绝，初始Trust内准确metadata-only许可才Gone/evidence_available=false。既有BodyGone/ObjectHolder不得复活，空policykey不抹除其它policy duty。

## 4. 新源码资格与旧证据

这次修改的是共享producer测试源码，**c11fd归档字节与旧N/F/R不覆盖新producer**。先静态最小diff/26pins，再按独立grant做新normal→新fence→配对race，失败即停；产品可能直接正确，无需制造业务RED。之后在新固定source跑受影响旧五个父场景的原正常/故障/race路径：durable SIGKILL、precommit SIGKILL、confirmed-commit replyloss、Save-withdrawal orphan、installedPut lateFinish。可按既有有限入口顺序分组，不扩业务时间，不把“新gate不走旧tag”当可免回归证据；无需借此重跑无关全产品矩阵。

全程Borrow-before-Start、preopen身份登记、现三pipes及exact scope保持。任何新firstClose未知独立保留；历史8 UNKNOWN绝不由新bodyALLACK或groupAbsent补证。原scope不清洗，不改旧producer/archive SHA。独立published secondary copy、offline恢复、真实ENOTEMPTY残留留后续各自vertical；本决定不接受06六AC或whole04。
