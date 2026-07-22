# T3：HTTP server 与安全中间件

## 目标

实现统一 JSON error、session 鉴权、CSRF、安全头、health/ready 和严格 SPA/API 路由边界。

## 步骤

1. 建立 server/config/dependency 边界；
2. 注册 OpenAPI operation；
3. 实现 auth/CSRF/recovery/request-id/security headers；
4. 实现 health/ready 与 SPA asset/fallback cache；
5. 用 `httptest` 覆盖匿名、过期、缺头、404 与 panic。

## 进展

- 已完成。所有契约 operation 由标准库 `ServeMux` 显式注册；通用中间件、auth handler、root probes、静态资源缓存与 `/api/*` JSON 404 已落地。
- `internal/api/server_test.go` 覆盖匿名边界、CSRF、session、panic 脱敏、SPA fallback 和 ready 状态。
