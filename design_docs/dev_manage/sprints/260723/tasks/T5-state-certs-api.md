# T5：状态、统计、系统与证书 API

## 目标

安全暴露 M-STATE 归一化快照/有限序列与 Envoy Prometheus，并实现私钥只进不出的托管证书库。

## 步骤

1. status summary/listeners/clusters/endpoints/routes/certs；
2. stats overview/series 与 Prometheus 流；
3. system info；
4. 证书解析、配对、原子 0600 文件写入和引用删除保护；
5. 响应泄漏与错误路径测试。

## 进展

- 已完成。状态 API 返回 last-known 归一化对象与限定窗口序列；Prometheus 是唯一鉴权后的 Envoy 透明读取，失败内容脱敏为稳定 502 error。
- 托管证书库完成 X.509/私钥配对校验、同 data-dir 原子 rename、私钥 `0600`、SQLite 元数据索引、`Listener/<name>` 引用保护；编译器可将 `ref` 解析为托管证书文件。
- 定向 test/race、`go vet ./...`、golangci-lint v2.12.2 均通过。
