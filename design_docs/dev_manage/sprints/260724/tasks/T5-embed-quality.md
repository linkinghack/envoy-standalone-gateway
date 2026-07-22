# T5：嵌入与质量收口

## 目标

完成无障碍/窄屏审计、Go embed、browser smoke 和 S6 全门禁。

## 步骤

1. 键盘、focus、语义、对比度、reduced-motion 审计；
2. 360px/768px/desktop 响应式检查；
3. build 产物同步与 Go embed；
4. browser smoke 和 SPA/API route tests；
5. 全量门禁、roadmap/index 回写。

## 进展

- 2026-07-22：完成。语义/focus/reduced-motion、390px 移动导航和桌面布局完成审计；生产产物嵌入单一 Go 二进制，SPA 深链测试通过。
- 验证：Vitest 5/5；Playwright 桌面/移动共 5 passed、1 个移动专属断言在桌面按预期 skipped；Go build/test/race/vet/golangci-lint 全通过。
