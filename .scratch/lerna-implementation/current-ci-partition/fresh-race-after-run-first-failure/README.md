# 合入 Run 后首次新竞态分组失败

固定候选 `860df74ef610c1e7280b9c6abd2974ae0677b739` 含实际 Run 整合 `b9db5cc`。root 全文阅读146行原输出，独立核验36项原件与归档字节相等，共546127字节，见[索引](archive-proposal-index.json)和[核验](root-independent-archive-audit.json)。原执行仅一次，实际 parent3915789／start16414692、Wait exit2、当前组不存在，随后 STOP／RELEASE。

Recovery 86.899秒、closure 50.522秒、三个 Content 33／32／32组64.804／93.051／96.118秒均成功。Durable60包在120.066秒触发原2分钟期限；栈停在末项 setup 的PG认证，不证明单项业务期限或认证根因。other43及fixtures未开始。原实际五个 Component selector覆盖158项，未声称完整201项执行或完整发现并集。9个子操作0015–0022成功、0023失败；counter24及新ledger1–982保留，原共享1445秒期限未续期。

旧 ledger1436、counter15、旧首次失败与 sticky FS UNKNOWN 不修改；当前组或路径不存在不补首次 Close。必要下一决定为仅将 Durable按原发现顺序拆30／30，原包120与单次总体1445不改；这是候选修正方向，尚无新整体通过。
