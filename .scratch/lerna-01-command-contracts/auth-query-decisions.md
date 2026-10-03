# Ticket05：受信查询端口、超时与可变回调隔离

2026-10-03。基线a96f49d，工作树 `/tmp/lerna-worktrees/contract-05`。这是纯库和可控事实源的实现细化；不新增ADR，不修改代码。

## 端口形状

接受以下最小消费方接口：

```
ReadAuthorizer.AuthorizeCommandRead(ctx, SubjectBinding, CommandRef)
OwnerDirectory.Resolve(ctx, OwnerRef) -> ResolvedCommandOwner{Owner, Reader}
```

Reader采用ticket04已有CommandFactReader。Directory直接返回宿主装配的reader，当前不需要opaque endpoint再配factory回调。先冻结原OwnerRef，再解析；结果Owner必须准确等于冻结值，不能按默认owner替代。测试更新同owner对应的Reader可证明逻辑owner固定与提供者替换，仅代表同进程受信目录与注入实现；不宣称真实地址、认证网络或分布式接替已验证。

ReadAuthorizer必须先于目录/事实读取，对确切原CommandRef判定权限；权限拒绝对存在和不存在同样表现。SubjectBinding只能由调用方受信宿主注入；payload和target不能升级它。

## Go：调用方必须提供有限deadline

保留实现者提出的入口前提：调用ctx必须有有限deadline。缺失deadline、已经取消或已经超时，立即fail closed，不调用authorizer/directory/reader。公开错误不能包含原始provider细节；缺失deadline是宿主调用条件不满足，不代表原业务命令被拒绝。

这是一项明确的库API使用条件，README示例应展示context.WithTimeout和defer cancel。无需为了隐藏调用错误自动加入一个秘密默认超时。后续宿主负责提供符合其SLO的期限。

所有端口接受并传递同一ctx；端口合同要求其I/O设置ctx对应截止并及时响应取消。每个等待之后、返回可读事实之前重新检查ctx，不能把取消后迟到成功当作仍有效的正常观察。

**保证边界：** Go context是合作取消，不会强制终止任意实现。当前不得无界创建goroutine，以超时后遗留后台goroutine的方式假装取得绝对限时。纯库没有workers，接入的Go提供者必须遵守端口取消合同；测试使用会响应ctx的阻塞事实源。若提供者故意忽略ctx且永久阻塞，本库不声称可以强行终止或保证调用返回；后续具体适配器必须以实际I/O期限验证这个条件。

## TypeScript：配置及全流程限时

受信宿主提供必填本地配置：

```
interface CommandReadOptions {
  maxReadDurationMs: number; // 整数，1..60000
}
```

这是库调用/宿主配置，不是线上Schema字段；允许JS number表示有界毫秒并不改变线协议整数必须用字符串的规则。配置非法时拒绝初始化或调用，不取整、不静默补默认值。调用方还必须提供AbortSignal；信号已abort时不启动任何provider。

以单个总时限覆盖authorizer、directory和reader整个异步流程，不能每进入下一步就重新计时。内部AbortController连接外部取消和maxReadDurationMs timer，将derived signal传给各端口。每次等待provider时使用同一截止Promise/取消Promise参与race，不能仅传AbortSignal后await无限不settle的Promise。

race保证事件循环正常推进时的异步等待可以有限返回，但不保证第三方工作已停止；也不能抢占同步死循环或阻塞事件循环的回调。不要把读取超时解释为不存在、原写未提交或外部效果未发生。

结束时clearTimeout并移除外部signal listener；所有provider Promise的迟到拒绝必须已被观察，不能产生unhandled rejection。超时后不启动下一阶段，迟到成功不能再次决定结果，不更新owner或原引用。

## 时间的三个不同含义

1. 原写命令accept_before：保持原值，过期不使已保存原回执失去查询资格。
2. 本次command.get信封accept_before：该读请求开始接纳的业务截止，由已注入clock判断。
3. ctx deadline/maxReadDurationMs：本次调用的运行耗时限制；不改写上述任一绝对截止。

业务clock可控，运行耗时由Go deadline/JS timer控制；不能用可停止的测试业务clock冒充真实有限I/O期限。

## 失败表达及不泄露

- 明确的授权拒绝：闭合rejected/forbidden，不含原记录、对象内容、进展或backend错误字符串。
- 提供者异常、超时、目录不可用或错误owner：闭合unavailable/dependency_unavailable，保留调用方已提交的原CommandRef，不尝试别的owner。
- 缺少受信主体按forbidden；宿主无有限deadline或无合法TS时限配置属于调用配置失败，可用本地可判断错误表示；若已经映射为线上查询结果，仅使用既定公共dependency_unavailable，不返回原始错误文本。配置失败不能伪装成原CommandReceipt.rejected。
- 运行时错误的本地cause可保留用于宿主诊断；public error只投影闭合code，不默认打印完整用户载荷。

## 回调隔离

批准clone参数及保留primitive snapshot：先按Schema验证并复制SubjectBinding与CommandRef，冻结tenant/owner/command_id原值；给每个受信可替换端口单独副本，尤其复制delegation_chain。后续身份比较总是使用入口保存的原始值，不能使用前一回调可能改写的对象。

Directory返回的Owner和Reader配对需马上读取；Owner复制并和原snapshot比对，不能先信任provider可变对象、再在下一await后重读。Reader返回事实也必须复制/严格验证之后再交付，避免provider保留引用随后改写对外固定回执。Go切片与[]byte同样按所属责任复制；不要仅复制包含slice的浅层struct。

无需通用deep-freeze框架；合同结构有限，使用现有严格encode/decode或明确字段复制即可。TS只clone数据，不尝试clone函数、Reader实例或AbortSignal。

## 验证

保留正常授权路径；加入授权拒绝存在/不存在一致、错误owner、原owner不可用、同owner Reader更新、提供者改写输入与输出、有限等待和外部取消正常/失败对照。Go使用遵守ctx的阻塞reader；TS用永不settle的异步Promise证明race返回，并验证迟到reject不泄漏。测试不需要真实网络或后台workers，也不扩展为强制终止第三方执行器的承诺。
