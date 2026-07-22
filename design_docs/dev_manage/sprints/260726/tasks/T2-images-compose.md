# T2：OCI 镜像、compose 与健康检查

## 目标

交付管理面-only 与 all-in-one 两种非 root 镜像、可复制 compose 示例和无 shell 健康检查。

## 步骤

1. `esgw healthcheck` CLI 与单测；
2. 多阶段 `esgw`/`esgw-all-in-one` Dockerfile；
3. 容器配置、entrypoint、volume/端口/healthcheck；
4. compose 启动和镜像元数据 smoke；
5. 更新 trace 并独立提交。

## 进展

- 已完成：新增有界、拒绝重定向的 `esgw healthcheck`，以 HTTP 2xx 为 readiness 成功条件并覆盖超时/错误参数单测。
- 交付 `esgw` scratch 管理面镜像与基于官方 Envoy 1.39.0 的 all-in-one 镜像；两者固定 uid/gid 65532、exec-form 健康检查与持久数据卷。
- all-in-one 与 separated xDS compose 均通过配置解析；prebuilt smoke 复用同一运行层，真实启动两类容器并核验非 root、readiness 与 Envoy 版本。
- Docker Desktop 拉取 Go builder 时外部 registry OAuth 连接不稳定；运行层 smoke 不依赖该外部拉取，生产多阶段 Dockerfile 保留在最终全量门禁重试。
