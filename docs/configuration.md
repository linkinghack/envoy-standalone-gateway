# 配置与部署拓扑

ESGW 有两类配置：`esgw.yaml` 控制进程和交付方式；`dataDir/config.d/*.yaml` 是 Gateway/Listener/Route/Upstream/Policy/EnvoyResources 对象的配置真源。两者都使用严格 YAML，未知字段会使启动或校验失败。

## 进程配置

```yaml
dataDir: /var/lib/esgw
deliver:
  mode: xds                     # xds | static
  xds:
    listen: 127.0.0.1:18000     # M1 仅允许 loopback ADS
    nodeID: esgw-node
    nodeCluster: esgw
    adminAddress: unix:///run/esgw/envoy-admin.sock
    ackTimeout: 15s
  static:
    outputPath: /var/lib/esgw/envoy/envoy.yaml
proc:
  enabled: true                 # false = Envoy 由外部管理器负责
  envoyPath: /usr/local/bin/envoy
  baseID: 0
  liveTimeout: 30s
  drainTime: 10m
  parentShutdownTime: 15m
  adoptPolicy: keep             # keep | restart
  restartBackoff:
    initial: 1s
    max: 30s
    resetAfter: 1m
    giveUpPer10m: 5
api:
  listen: 127.0.0.1:8080
  topology: standalone          # standalone | sidecar | central
```

`static` 模式要求 admin 使用绝对 Unix socket。`proc.enabled=true` 时 ESGW 发现并检查 Envoy 版本，生成启动配置、记录 `/var/lib/esgw/run/proc.json`，并在管理面重启后接管仍存活的数据面。

## 四种部署组合

| 拓扑 | xDS | static |
|---|---|---|
| ESGW 托管 Envoy | 发布通过 ADS 生效，不重启 Envoy | 原子替换文件并执行 Envoy hot restart |
| 外部管理 Envoy | 外部 Envoy 连本机 ADS；ESGW 不发进程信号 | ESGW 只写文件；必须由外部管理器显式重启 Envoy |

外部 static 的“文件已生成”不表示运行中 Envoy 已生效。控制台会保留这个边界，不会冒充进程管理器。

## 网关对象

最小 HTTP 代理由 Listener、Route、Upstream 三个对象组成，见 [`quickstart-gateway.yaml`](../packaging/examples/quickstart-gateway.yaml)。文件可含多个 `---` 分隔的对象。常用约束：

- `metadata.name` 是稳定引用名；
- Listener 端口为 1–65535，HTTPS/TLS 必须配置证书；
- Route 按规则顺序首个命中，动作只能是后端、重定向或直接响应之一；
- Upstream 的 `endpoints`、`dns`、`kubernetesService` 三选一；M1 不实现 Kubernetes 自动发现；
- 时长使用 Go duration，如 `500ms`、`15s`、`2m`。

完整协议定义见 [`design_docs/system_design/260716-1-gateway-config-protocol-v0.md`](../design_docs/system_design/260716-1-gateway-config-protocol-v0.md)。
