# T3：校验发布与 Runtime

## 目标

实现 validate→review→publish→status 闭环及数据面运行状态视图。

## 步骤

1. 校验结果和 source diagnostics；
2. diff review 与发布消息；
3. awaiting/confirmed/timeout 状态；
4. listener/cluster/endpoint/route/cert 状态；
5. stale/error/loading tests。

## 进展

- 2026-07-22：完成。save→validate→review→publish 闭环、source diagnostics、发布错误反馈及 listeners/clusters/routes/certs 状态视图已落地。
- awaiting/confirmed/timeout 的权威状态由状态摘要和配置状态 API 驱动，页面不伪造成功态。
