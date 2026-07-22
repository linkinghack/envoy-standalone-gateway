# T1：OpenAPI 契约与生成边界

## 目标

建立 S5 可评审、可机器校验的 API 契约真源，冻结 P0 operationId、请求响应与统一错误模型。

## 步骤

1. 编写 `api/openapi.yaml`，覆盖 requirements 中全部 S5 路径；
2. 配置固定版本生成命令，生成 Go types/server interface；
3. 建立 spec operationId 与路由实现一一对应的 contract test；
4. 加入 spec lint 与生成 clean-diff 门禁；
5. 运行 build/test/vet/lint 并回写。

## 进展

- 2026-07-22：任务开始。
