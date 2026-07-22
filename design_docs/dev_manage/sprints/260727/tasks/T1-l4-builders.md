# T1：L4 语义、TCP/TLS/UDP Builder 与 golden

## 目标

把协议中已存在的 `Route.forward` 从 unsupported 路径提升为完整 IR，覆盖 TCP、TLS passthrough 和 UDP。

## 执行步骤

1. 盘点 F1/F2 当前 HTTP 假设，补 Listener/Route/Upstream 协议组合校验与定位错误；
2. 实现 TCP proxy filter、TLS inspector/SNI filter chains、UDP listener/filter；
3. 保证 cluster/endpoints 与 HTTP 共用、资源名和排序确定；
4. 新增单测、错误表、static/xDS golden；
5. 跑三版本 validate，更新任务进展并独立提交。

## 验收标准

- 三种 L4 协议合法配置生成 PGV 合法 Envoy v3 资源；
- HTTP rule/Policy 与 L4 forward 的所有非法混用均在 link/build 阶段定位；
- TLS SNI 缺失、重叠、确定性顺序有直接测试；
- golden 在 Envoy 1.37.5、1.38.3、1.39.0 全部 validate。

## 进展

- 已完成：
  - TCP/TLS passthrough/UDP Builder、L4 link 约束和 F6 typed filter cluster 引用闭合；
  - `testdata/l4` static/xDS golden，Envoy 1.37.5、1.38.3、1.39.0 共 21 次 static validate 全通过；
  - `e2e/l4/run.sh` 真实验证 TCP、两条 TLS SNI 分流、未知 SNI 拒绝和 UDP 回包；
  - `go test ./...`、`go vet ./...`、golangci-lint 与 compile/golden race 通过。
