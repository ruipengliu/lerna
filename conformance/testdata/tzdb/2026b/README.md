# 固定 TZDB 参考字节

此目录复制本机已安装 Debian `tzdata 2026b-0+deb13u1` 的原始 TZif 与完整 `tzdata.zi`，没有修改版本标记或用旧数据库重命名。`manifest.json` 固定逐文件 SHA256 与原 UTC 链接；`COPYRIGHT` 保留原 public-domain 声明。

仅包括参考配置使用的 UTC、Asia/Shanghai、America/New_York、Europe/London，以及 UTC 指向的 Etc/UTC。通过 `HARNESS_TEST_TZDB_ROOT` 显式取得，`scripts/check_toolchain.py` 核字节和版本。它是确定的本机/CI 测试数据，不代表更多时区、所有平台或生产规模已资格通过。
