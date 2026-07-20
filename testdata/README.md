# testdata — golden 快照用例与测试证书（编译层 §8.1 测试策略）

## 布局

```
testdata/<case>/input/*.yaml      协议 YAML 输入（编译零错误的正例）
testdata/<case>/want-static.yaml  static 模式产物快照（static.Render）
testdata/<case>/want-xds.json     xds 模式产物快照（deliver.SnapshotJSON）
testdata/bootstrap-xds/esgw.yaml            接入 bootstrap 的 esgw.yaml 输入（S2 T4）
testdata/bootstrap-xds/want-bootstrap.yaml  接入 bootstrap 产物快照（xds.RenderBootstrap）
testdata/errors/<case>/input/*.yaml      错误用例输入
testdata/errors/<case>/want-errors.json  错误集合快照（stage/severity/SourceRef/message）
testdata/certs/                   自签测试证书（仅测试用，见其 README）
```

## 正例用例集

| 用例 | 内容 |
|---|---|
| `s1` | 协议 §8.1 多域名 TLS 反代原文（证书路径替换为 testdata/certs，见 `s1/README.md`） |
| `s2` | 协议 §8.2 API 网关原文 + 同构补全（https Listener 与三个省略的 Upstream） |
| `patch-merge` | 协议 §7.1 envoyPatch merge 形态 |
| `patch-jsonpatch` | 协议 §7.1 envoyPatch jsonPatch 形态 |
| `envoy-resources` | 协议 §7.2 EnvoyResources（allowOverride 默认 false） |
| `envoy-resources-override` | 协议 §7.2 allowOverride: true 替换态 |

错误用例（`errors/`，T3/T5 表驱动反例的正式化）：`dangling-upstream`、
`listener-address-conflict`、`hostname-conflict`、`https-missing-tls`、
`patch-bad-jsonpatch`、`envoyresources-conflict`。

## 运行与刷新

```sh
go test ./internal/golden            # 比对快照
make golden-update                   # 刷新全部快照（diff 必须人工评审）
```

## 确定性约定（验收 A6 红线）

- 输入 YAML 中的证书路径一律为相对仓库根的相对路径（`testdata/certs/...`）；
  golden 测试在仓库根目录运行（测试内 chdir），`esgw compile` 同样从仓库根执行。
- 快照与错误文件中的路径因此与机器无关；同一输入两次编译必须字节级一致。
