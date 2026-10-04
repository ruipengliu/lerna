# 票05原政策清理责任与物理确认

root 完整读取并采用[原决定](decision.md)，原始字节及准确源码截点见 [provenance.json](provenance.json)。原报告只读待验收测试；随后真实首red0.406s在出版、save撤销、实际政策传播和重开之后确认没有建立seal。后续绿色实现尚在独立工作树中准备。

首条实际政策清理链使用原准确change key、完整保存主体／用途、ref和责任deadline建立不可变关联。同一事务内共享既有封闭步骤、完整holder集合与原Job，再精确比较原pending责任。当前保存失效必须有明确因果，不能把 `CheckPolicy` 的nil结果解释为删除许可。

Lifecycle.Step只有在全部原holder独立确认擦除、当前Claim／管理资格／原deadline仍合法时，才同事务记录原责任 `erased` 并完成工作。staging与primary的gone允许最小元数据观察，但不能代替secondary确认。两个完成出口均执行最终新鲜时间检查。

主动seal的政策key为空；已有不同seal的身份和期限不得重写，也不得根据同objectID清除其他责任。历史维护预算耗尽后不新建物理清理Job，不自动续期。本首链不代表多触发、祖先、期限、确认未知或全集分页已通过。

普通责任写入继续保留pending和历史holder union；ObjectHolder当前事实的相邻修正另行验证。未增加公共delete、Grant或第二套清理框架，票03／05与切片04尚未验收完成。
