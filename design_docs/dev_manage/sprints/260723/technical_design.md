# Sprint 260723 技术设计

## 1. 模块边界

- `api/openapi.yaml`：HTTP 契约唯一真源；`internal/api/gen` 承载生成的 boundary types/interface；
- `internal/auth`：密码、用户、session、限流，不依赖 HTTP；
- `internal/certstore`：托管证书文件与索引，不依赖 HTTP；
- `internal/api`：路由、中间件、DTO 映射与领域 service 组合，不含领域规则；
- `internal/core`：唯一生产组合根，构造 Store/Deliver/State/Conf/API 并管理生命周期。

## 2. HTTP 与安全顺序

请求顺序固定：request-id/恢复 → 安全响应头 → 路由 → session 鉴权 → CSRF（写请求）→ handler。默认不发 CORS 头；Cookie 认证不接受 query token。API error 统一为 `{"error":{"code","message","details"}}`，500 不回显内部错误。

bootstrap/login 是仅有的匿名写 API，但仍要求 `X-ESGW-Request: 1`。bootstrap 窗口由启动时刻起 30 分钟；`ESGW_INITIAL_ADMIN_PASSWORD` 可在 server 对外监听前初始化账号。

## 3. 文件写入

整体 draft 替换和证书写入均先在 data-dir 内的临时文件/目录完成校验、fsync/close，再 rename；只接受清理后的相对路径，禁止绝对路径与 `..`。对象 CRUD 复用 `conf` 的 Origin 原子替换；新对象写入 API 管理的单对象文件。

## 4. API 依赖接口

API server 通过最小接口消费 Auth、Config、State、Certs 与 SystemInfo；测试使用内存 fake。生产 adapter 才调用 `conf.Publisher`、`state.Service` 和 `store.Store`。这使 HTTP 契约测试不要求真实 Envoy，同时 S5 收口必须有一条组合级发布测试。

## 5. OpenAPI 门禁

提交 spec 与生成文件。`go generate` 使用固定版本的 oapi-codegen；CI 执行生成后 `git diff --exit-code`。在当前执行环境无法获取生成器时，先由 contract test 验证 operationId、路径和 handler 注册表一一对应，生成 clean-diff 在工具可用后补证，不能伪称通过。

## 6. 关键失败语义

- draft hash 冲突、active publish、重复 bootstrap：409；
- 请求/YAML/路径非法：400；编译校验结果为合法查询响应，`ok=false`；
- session 缺失/过期：401；CSRF/角色：403；
- Envoy admin 暂时不可达：状态响应保留 last-known + `stale=true`；Prometheus 流返回 502；
- 领域错误写入结构化服务端日志，客户端只得到稳定 code。
