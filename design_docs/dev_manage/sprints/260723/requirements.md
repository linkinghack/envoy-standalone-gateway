# Sprint 260723 需求：M-API 与安全管理面

## 目标

落地路线图 S5：以 OpenAPI 为契约真源，把已完成的配置域与状态域安全地暴露为管理 REST API，并提供后续 SPA 的同源静态资源服务边界。

## 范围

- `api/openapi.yaml`、生成边界与契约一致性测试；
- 首次引导、本地 admin、Argon2id 密码、服务端 session、登录限流；
- 鉴权、CSRF、自定义 JSON 错误、health/ready 与 SPA fallback；
- 配置 draft/object/schema/validate/compiled/diff/publish/status/version/rollback P0 API；
- 状态、有限统计、Envoy Prometheus、系统信息 API；
- 托管证书上传/列表/详情/删除，私钥只进不出；
- M-CORE 生产装配与 HTTP 集成测试。

## 非范围

- P0 控制台页面本身（S6）；
- static hot restart 与 M-PROC（S7）；
- OIDC、多用户/RBAC、审计 UI（P2）；
- 完整 M2 统计、k8s disco 与 Helm。

## 验收标准

1. OpenAPI 中 S5 P0 operation 全部有 handler，未知 `/api/*` 始终 JSON 404；
2. 除 bootstrap/login/health/ready 外的管理端点均需有效 session；
3. 所有带副作用请求缺少 `X-ESGW-Request: 1` 时返回 403；
4. bootstrap 只允许一次，密码少于 12 字符拒绝，登录错误不泄露账号是否存在；
5. session cookie 为 HttpOnly + SameSite=Lax，数据库只保存 token SHA-256；
6. HTTP 可完成草稿读取、对象 CRUD、校验、发布、生效状态查询与受保护回滚；
7. state API 只返回归一化数据，不透明代理仅限鉴权后的 Prometheus 流；
8. 证书私钥写入权限 0600，任何 GET/错误都不返回私钥；
9. `make build test lint`、race/vet、API contract/integration tests 全绿。
