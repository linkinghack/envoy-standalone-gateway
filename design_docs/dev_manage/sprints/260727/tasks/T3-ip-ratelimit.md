# T3：IPAccess 与动态 key 本地限流

## 目标

实现 IP allow/deny 和按 client IP/header 隔离的本地限流，删除现有共享桶语义降级。

## 步骤

1. Envoy 三版本 descriptor spike；2. RBAC per-route 规则；3. 动态 rate-limit action/descriptor；4. 归一化、排序与冲突测试；5. 多来源真实流量 e2e 与提交。

## 进展

- 进行中：完成 IPAccess RBAC per-route Builder（allow 白名单、deny 优先、CIDR mask/排序/去重）与单测；下一步完成真实来源流量和动态 rate-limit descriptor 三版本 spike。
