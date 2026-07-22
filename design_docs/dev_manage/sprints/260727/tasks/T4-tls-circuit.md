# T4：downstream mTLS、熔断与校验收紧

## 目标

完成 Listener clientCA 与 Upstream circuit breaker 的 P1 语义、错误定位和真实行为证据。

## 步骤

1. CA/客户端证书编译与校验；2. require_client_certificate；3. 熔断正数/上界规则；4. proto/golden；5. TLS/并发流量 e2e 与提交。

## 进展

- 已完成：`clientCA` 在 F2 校验 PEM/file 并精确定位 `spec.tls.clientCA`；F3 生成 trusted CA validation context 和 `require_client_certificate=true`。
- 已完成：F5 static 保持 CA 文件 DataSource；xDS 提取 `ca/<listener>` validation-context Secret、写入 SourceMap 并生成 ADS/SDS 引用，消除数据面共享管理面文件路径的隐含前提。
- 已完成：`maxConnections/maxPendingRequests` 协议范围固定为 `1..2147483647`，零、负数和 int32 溢出均在编译前拒绝；Envoy Cluster 使用 DEFAULT priority 并有 proto 断言。
- 已完成：新增 `testdata/mtls-circuit` static/xDS golden，覆盖 CA Secret、`require_client_certificate` 与两个 circuit-breaker 阈值；三个 Envoy 版本 validate 通过。
- 已完成：`e2e/mtls-circuit/run.sh` 在 Envoy 1.37.5、1.38.3、1.39.0 分别验证缺失/不可信客户端证书拒绝、可信 ClientAuth 证书通过，并以 8 路并发触发真实 503 overflow。

## 完成判定

downstream mTLS 与熔断已从“字段透传”升级为协议校验、static/xDS 形态、proto 断言和三版本真实行为闭环。T4 完成，后续进入 T5 协议发行与 conformance。
