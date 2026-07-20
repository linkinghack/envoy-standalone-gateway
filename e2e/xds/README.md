# e2e/xds — ADS 模式真实流量 e2e（Sprint 260720 T5，验收 A2/A3/A4/A5/A7）

`run.sh` 用 docker compose 拉起 **esgw serve（xds 模式）+ 真实 Envoy + 两个
`hashicorp/http-echo` 后端**，Envoy 以 `bin/esgw bootstrap` 导出的接入
bootstrap 连上 esgw 的 ADS，跑通 S1 场景真实流量。与 `e2e/`（static 模式）
并存，二者互不影响（端口约定相同，错开运行即可）。

## 断言

| 断言 | 内容 | 验收锚点 |
|---|---|---|
| A7 | Envoy 以导出的接入 bootstrap 正常启动（admin `/ready` = LIVE） | A7（e2e 部分） |
| A2 | admin `/config_dump`：LDS/RDS/CDS/EDS/SDS 五类型资源在册，`version_info` == IR.Version | A2 |
| A4 | 同一配置目录：`compile --mode xds` 产物 `version` == golden `testdata/s1/want-xds.json` `version` == ADS `version_info` | A4（M0 第 3 项闭环） |
| A5 | esgw 日志可见五类型 ACK（Debug 级，esgw 以 `-log-level debug` 启动） | A5（日志部分） |
| A3 | 四断言同 static e2e：www→www-backend、blog→blog-backend、http→301 https、SNI 集合外握手失败 | A3 |

## 运行

```sh
make e2e-xds        # 或 e2e/xds/run.sh
```

依赖 docker 与 go（交叉编译容器内 esgw 二进制）；默认 `go test ./...`
不触发（工程基线 §3）。端口可用 `E2E_HTTP_PORT`（18080）/
`E2E_HTTPS_PORT`（18443）/`E2E_ADMIN_PORT`（19901）覆盖。

## 关键决策

- **共享网络命名空间（pod 形态）**：四个服务经
  `network_mode: service:esgw` 共享 esgw 的 netns。由此 esgw 的
  `deliver.xds.listen` 保持 `127.0.0.1:18000`（loopback 硬校验的合规形态，
  与生产默认一致），S1 输入的 `127.0.0.1:3000/4000` 端点与
  `testdata/certs/` 相对证书路径**零改写**直接可用——static e2e 的
  sed/awk 弥合在这里完全不需要，`make e2e-xds` 与 golden 消费同一份输入。
- **esgw 运行形态**：容器内运行（scratch 镜像 + run.sh 预编译的静态
  linux 二进制，`CGO_ENABLED=0`，架构对齐 docker server）。选容器内而非
  宿主编排，是因为 loopback 硬校验下宿主 loopback 对 linux 容器不可达，
  而共享 netns 后容器内 loopback 语义与生产单机部署完全一致。
- **adminAddress 用 `0.0.0.0:9901`**：Envoy admin 只绑 127.0.0.1 时 docker
  端口发布（DNAT 到容器 eth0）不可达；发布侧已限定 `127.0.0.1:19901`。
  adminAddress 无 loopback 硬校验（仅 `deliver.xds.listen` 有）。
- **EDS 版本观测**：Envoy 的 `/config_dump` 中 `DynamicEndpointConfig`
  不携带 `version_info`（v1.39.0 实测），EDS 的版本以 esgw 侧 EDS ACK
  日志（`version=<IR.Version>`，A5）为证；其余四类型直接断言
  `version_info`。
- **时序**：`depends_on` 仅保证启动序；就绪一律由断言脚本重试等待
  （`/ready`、config_dump、ACK 日志、流量断言各带 30s 重试），无固定 sleep。
- **Envoy 镜像版本**：默认取 `internal/version.EnvoyMatrixVersions` 最后
  一个版本（与 static e2e、validate 矩阵同源），`ENVOY_IMAGE` 可覆盖。

生成物（linux esgw 二进制、bootstrap.yaml）落在 `e2e/xds/generated/`
（已 gitignore）。
