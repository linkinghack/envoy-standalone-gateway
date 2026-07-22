# Sprint 260725 需求：static 下发与 Envoy 进程托管

## 目标

落地路线图 S7：补齐 static 运行时下发和 M-PROC，使托管拓扑可安全启动、接管和热重启 Envoy，仅下发拓扑严格不管理外部进程，并在管理面重启或配置失败时保护最后仍在服务的数据面。

## 范围

- `proc.*` 与 `deliver.static.*` 严格配置、默认值和安全校验；
- Envoy 二进制发现、版本区间检查、进程记录与存活接管；
- spawn、信号/退出分类、stderr 尾部、指数退避、稳定窗口与 degraded；
- static 确定性渲染、同文件系统原子写、目录持久化；
- hot restart epoch 单调协调、LIVE 探测、失败回杀新 epoch且不触碰旧 epoch；
- xDS/static × 托管/仅下发四组合装配与恢复语义；
- 单元/故障注入/真实 Envoy static hot restart e2e。

## 非范围

- 文件级动态资源热加载（DL2）；
- 跨主机 xDS mTLS、多节点和 Kubernetes 进程管理；
- systemd、容器镜像和发行安装脚本（S8）；
- 自动杀死身份不确定的外部 Envoy。

## 验收标准

1. 低于支持区间的 Envoy 被拒绝，高于区间告警继续；发现顺序和错误可排障；
2. `proc.json` 与 live/epoch 一致时接管且不重启，默认 `keep` 不杀死不确定进程；
3. 连续崩溃指数退避并在阈值后 degraded，稳定运行后重置；
4. static 非法/写入失败不替换 last-good，原子落盘不会暴露半文件；
5. hot restart 只在新 epoch LIVE 后受理，失败只回杀新 epoch，失败 epoch 不复用；
6. `proc.enabled=false` 不 spawn、不 signal、不接管；static Apply 明确只表示文件落盘；
7. 管理面重启不杀死已服务 Envoy；四组合有装配测试；
8. 全仓 build/test/race/vet/lint 及真实 Envoy static hot restart e2e 通过，无法执行的外部门禁必须如实记录。
