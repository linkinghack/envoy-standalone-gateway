# T2：extAuth HTTP/gRPC

## 目标

无损实现外部鉴权服务、fail-open 和 route 级关闭，并明确 Envoy 单 filter 配置约束。

## 步骤

1. 收紧协议地址校验与默认；2. 归一化 Listener 内有效 provider；3. 生成 auth cluster/filter/per-route disable；4. 单测允许/拒绝/故障/冲突；5. 真实 HTTP/gRPC auth e2e 与提交。

## 进展

- 待开始。
