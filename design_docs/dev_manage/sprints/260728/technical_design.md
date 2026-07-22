# Sprint 260728 技术设计

## 1. 发布与版本不变量

`Publisher` 的确认操作在同一 Store 事务中把目标版本设为 `effective`、此前 effective 设为 `superseded`、publish run 设为 `EFFECTIVE`。`CONFIRM_TIMEOUT` 保持终态但允许显式 recheck：重新登记同一 `IRVersion` 到 M-STATE，运行记录回到 `CONFIRMING`。任何 API 都不能直接伪造确认。

回滚不再调用会同步 `Confirm` 的便捷路径。`RollbackPublish` 先按 `force` 规则恢复历史源，再携带 `rollbackOf` 调用与普通发布相同的 `PublishWithBase` 内核，返回 `CONFIRMING`；版本写入时即持有 rollback 元数据。失败保留运行记录和当前草稿，便于用户修复/重试，不回写历史快照。

API 增加 publish run 列表/详情/recheck。响应公开状态、触发者、version、诊断、diff 和时间，不公开内部 DB 表细节。版本详情关联对应 publish run，使 UI 能区分 validation、delivery 与 data-plane confirmation。

## 2. 双视角 preview

新增 `POST /config/preview`，输入 `expectedResourceVersion`、mode 与是否 F7，输出：

- 当前 draft/effective 标识和完整 validation diagnostics；
- 规范化文件 `added|removed|changed` 与 unified patch；
- 将 base 与 draft 编译为同模式 IR 后，按 `(resource kind, name)` 归并的资源 `added|removed|changed`，changed 带 canonical protojson before/after；
- 目标 `IR.Version` 与资源计数。

资源 diff 只复用共用 Compile/IR，不建立第二套编译器。无 effective 版本时 base 为空。预览 token 与 `resourceVersion` 绑定，发布仍复检，避免“预览旧世界、发布新草稿”。历史版本 diff 同样支持 `view=source|resources`。

## 3. 表单与 YAML 双向编辑

文件仍是唯一真源。表单读取 `/config/objects`，保存走对象 PUT/DELETE；YAML 保存走 draft PUT。任一成功后失效整个 `config` query 域并重新读取另一视图，因此不存在客户端双副本合并。

表单字段目录来自已发行的 v1alpha1 JSON Schema：解析 `$ref`、object properties/required、array、enum/const 和 scalar 类型；`oneOf` 显示显式 variant 选择。对 `RawJSON`/Envoy patch 等自由 JSON 节点提供 JSON textarea 兜底，保持 escape hatch 无损。所有六种 kind 均支持创建、编辑和删除；未识别字段不得被表单保存静默删除，遇到无法表达的节点必须要求转源码视图。

## 4. 版本与发布控制台

新增独立 `Releases` 页面：顶部展示 draft→preview→delivery→confirmation 四阶段；下方为分页线性版本时间线。详情抽屉/面板提供 source/resource diff、原始源码、compiled snapshot 和发布证据。回滚默认只恢复为草稿供预览；“回滚并发布”必须二次确认 `force` 覆盖语义，随后轮询 publish run 到终态。

配置页发布按钮改为先获取 preview；有 error diagnostics、resourceVersion 漂移或零资源变化时不可发布。确认中状态不以成功色展示。

## 5. 统计模型

逐路由统计使用 Envoy `config.route.v3.Route.stat_prefix`，命名 `rt/<route>/<rule-name-or-index>`。该字段由真实已匹配 Route 自身计数，不复制或近似 match 语义；官方说明其指标位于 `vhost.<virtual-host>.route.<stat-prefix>`，与 virtual cluster 指标同口径。来源：[Envoy HTTP Route API](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/route/v3/route_components.proto.html)。

采集器只接收产品口径白名单，归一为四维：`route`、`cluster`、`listener`、`global`。cluster 使用完整 `us/<name>`，route 使用完整 `rt/<route>/<rule>`，不能用简单 `strings.Split` 截断带点名称。白名单覆盖请求/状态、延迟分位数、连接、连接失败/重试、健康成员和熔断剩余量，避免无关高基数 stats 占满 5000 序列上限。

查询层从相邻 counter 点计算 rate；当前值小于前值视为 Envoy 重启/reset，返回断点而非负数。成功率固定为 `1 - 5xx/total`，4xx 单独展示但不判服务失败。直方图标注“Envoy 窗口近似”。overview 返回聚合值和采集起点，series 返回有序点 `{timestamp,value|null}`，不把 ring 内部 `Head/Filled` 暴露为协议。

统计 UI 使用轻量 SVG 折线/面积图，不引入图表运行时；提供 5m/15m/1h/6h、route/cluster 切换、stale/reset/无数据状态以及请求量、成功率、2xx–5xx、P50/P90/P99、连接/健康/熔断卡片。

## 6. 验证策略

- Store/Publisher：状态事务、timeout recheck、并发、rollback metadata 和 superseded；
- diff/API：source/resource preview、历史双视角、OpenAPI contract/generation；
- compile/state：route stat prefix proto/golden，stats 名称归属、rate reset、overview；
- real Envoy：两 route/两 cluster 发流后 admin stats 与 API 序列独立；
- Web：schema form round-trip、preview/publish/confirm、history/diff/rollback、stats 窗口，桌面与移动端；
- 收口：Go 全门禁、Web check、三版本 validate、相关 static/ADS e2e 和 clean diff。
