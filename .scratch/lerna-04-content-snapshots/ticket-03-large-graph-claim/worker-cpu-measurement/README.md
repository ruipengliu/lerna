# 原四容量 B：一次竞态 CPU 采样与七个只读视图

准确产品28e3a468c86d4323d3b497836de23bb311a58952。全新五文件overlay仅在两大图实际Step加case/phase标签；原caller30s、Claim5s、test/outer120s不变。独立race编译0/absence，二进制SHA963905保存在provenance，二进制仍在原所有目录，不提交可执行文件。

ONE采样native3299533/start13781792/session30305实际exit1/absence。root全文159行：closure64在caller30截止时失败，Step3.385588s，末次可得DB时钟Claim仍余4.95321s；不能据此推断最终Claim时钟。static62实际Prepared COMMIT后，首次Publish的PrepareContent失败，Claim末超29.191ms，未readback或completed。65拒绝、单内容正常/拒绝及重复selector用例通过。采样增加开销，本次阶段不同于此前未采样race；此前失败保持原样，当前两个大图不接受。

同一个原profile七个pprof视图均实际0/absence，全部原argv、PID/starttime、日志哈希见analysis-outcomes；root全文读取并独立核对哈希与当前PIDabsence。实际78.40s wall、62.18s CPU；标签6.80s只覆盖两次Step，其中closure2.83s/static3.97s。pprof百分比以全部62.18s为分母，55.38s未标记样本不能强制归入某阶段；offCPU未测，父子cum不能相加。全局flat以racecall/tsan为主，两个Step的注册闭包/准确版本验证仍可见；这里只保存测量，不自行认定SQL或选择第二产品优化。

Go标准testing CPU原文件描述符的逻辑Close为UNKNOWN；独立资格FD的fsync/Close、native Wait及组absence不能替代原CloseACK。原profile/dev27/ino560161/SHA427bbc保持，所有未知范围保留，无cleanup。

编译provenance中的business_unrun/profile_missing是编译时事实；实际采样结果在profile-outcome。原STATIC命令清单不是授权或执行结果。全部原overlay/diff、采样和只读视图按准确字节保存；进一步必要修正等待真实热点裁决及单独TDD验证。

## 实测后的采用决定

root全文读取并采用[Astra下一窄决定](adopted-next-decision.md)，准确SHA2adc632b。仅单次registeredClosure栈内fullRef→成功VersionIdentity ID的<=65项复用，满只不保存；同排序/visit原位置与错误顺序保持，PG所有独立验证/授权/锁/时钟/两Tx不省。原race容量已是business red，不再制造人工red或profile。当前只授权静态最小实现，没有新green或容量接受；收益不得用嵌套cum相减预言。
