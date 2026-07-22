# T1：systemd 与原生安装生命周期

## 目标

交付安全、幂等、可测试的 Linux 安装/升级/卸载路径，确保管理面重启不误杀托管数据面。

## 步骤

1. unit、tmpfiles 与默认裸机配置；
2. install/upgrade/uninstall 脚本及 `DESTDIR` 测试模式；
3. 用户、目录、配置/私钥权限与 capability；
4. shell/unit 静态检查和事务/保留数据测试；
5. 更新 trace 并独立提交。

## 进展

- 已完成：交付 hardened systemd unit、tmpfiles、默认裸机配置以及幂等 install/upgrade/uninstall 脚本。
- `DESTDIR` 生命周期测试覆盖原子替换、配置保留、时间戳 binary 回滚副本、默认卸载保留数据和显式 purge。
- `sh -n`、`systemd-analyze verify` 与 `make packaging-test` 通过；离线 unit 检查仅提示宿主尚未安装目标 binary。
