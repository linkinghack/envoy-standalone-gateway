# T5：Route/Cluster 统计后端

## 目标

生成逐 route Envoy stats，修正维度契约并提供可直接消费的速率、成功率、状态、延迟和连接查询。

## 步骤

1. Route.stat_prefix；2. stats 白名单/完整名称解析；3. ring 点协议与 reset rate；4. typed OpenAPI overview/series；5. 真实 Envoy 两 route/cluster 流量。

## 进展

- 待开始。
