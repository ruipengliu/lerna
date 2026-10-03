# 固定历史解码合同

这四份 MethodContract 从实际历史提交的 Registry 导出。每份保留闭合 input/output Schema、原 method owner/kind 和原 SchemaDigest；`RetainDecoder` 在安装时重新核摘要和当前方法负责方。文件与本版本代码一起受信发布，不从远端发现、journal 正文或调用方输入推导合同。

| 来源提交 | 方法 | 原 SchemaDigest |
| --- | --- | --- |
| `c6e322827c6a8fb73aec56fddb8ab78c53c8e3cd` | executor.admission.install | sha256:a07a8a2a61c2c125a304724c5708970b9370139cc582c98b427dfa6b68a7d6b4 |
| 同上 | executor.admission.get | sha256:7c924b7b641c0e227b9c4197e3b4d50157416ac8bbd9f1bdc3629dda23ed8cad |
| `3bfd14f4b64f110a5ef167b871904a27b593ce10` | executor.admission.install | sha256:0c9769df5abe8a56baaa7f0b1e1910fe8e1340cbd4c661eada804c9580f5c50e |
| 同上 | executor.admission.get | sha256:941dd3c646c2adfd3b2ef7800685334b4282e92b731b27aa9254c5dac6197665 |

第一次升级增加准确 DeviceDatabaseID 绑定，第二次增加 SourcePolicy／SubjectRefs。旧 command journal 可用原 command 合同查询和解码原回执，不能借此建立旧版新准入。Query 不保存投递责任，仍使用当前合同；保留两份 query 合同使原成对输入／输出登记可核对。当前 `executor.content.get` 要求的来源许可没有旧 journal 路径，也不使用旧查询合同绕过来源门禁。
