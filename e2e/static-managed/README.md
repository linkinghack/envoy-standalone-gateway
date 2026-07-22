# static managed e2e

真实 Envoy 验收 static 托管路径：初始 epoch 0、API 发布触发 epoch 1、发布窗口持续流量、管理面单独重启后接管同一 Envoy PID，以及坏草稿不改动 live artifact/epoch/PID。

```sh
make e2e-static-managed
```

测试镜像通过 tar stdin 构建，运行时无 bind mount；这也让 CI/WSL 不依赖宿主目录共享。依赖 Docker、curl、jq 和官方 `envoyproxy/envoy:v1.39.0` / `hashicorp/http-echo` 镜像。
