# T3：IPAccess 与动态 key 本地限流

## 目标

实现 IP allow/deny 和按 client IP/header 隔离的本地限流，删除现有共享桶语义降级。

## 步骤

1. Envoy 三版本 descriptor spike；2. RBAC per-route 规则；3. 动态 rate-limit action/descriptor；4. 归一化、排序与冲突测试；5. 多来源真实流量 e2e 与提交。

## 进展

- 已完成：IPAccess 使用 per-route RBAC，落实 allow 白名单、deny 优先、CIDR mask/排序/去重。HCM 固定 `use_remote_address=true`、可信 hop 为 0，客户端自带 XFF 不能伪造来源。
- 已完成：`clientIP` 生成 `remote_address` action，`header:<name>` 生成 `request_headers` action；同 key 的空 value descriptor 使用 Envoy wildcard 动态桶。filter 显式 100% enabled/enforced，命中 descriptor 后不消费默认共享桶；header 缺失走默认桶。
- 已完成：新增 `rateLimit.maxKeys`（默认 10000、范围 1..100000）映射动态 descriptor LRU 容量；补齐空/非法 header name、burst 与缓存边界校验。
- 已完成：`testdata/ipaccess` 同时覆盖 RBAC、clientIP 与 header 动态 descriptor 的 static/xDS golden；Envoy 1.37.5、1.38.3、1.39.0 均通过 validate。
- 已完成：`e2e/policy/run.sh` 在三个版本逐一验证 allow、deny 优先、allow 外拒绝、双向 XFF 伪造无效、两个 client IP 独立 `200/200/429`、两个 header 值独立 `200/200/429`，以及缺失 header 默认桶 `200/200/429`。

## 完成判定

共享单桶降级和默认 0% 不执行均已删除；协议、编译产物、三版本真实行为一致。T3 完成，后续进入 T4 downstream mTLS 与熔断保护。
