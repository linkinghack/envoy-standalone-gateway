# T1：Web/auth 基线

## 目标

建立可持续的 React/Vite/shadcn-ui 工程、视觉 tokens、OpenAPI client、响应式 shell 和引导/登录/session 边界。

## 步骤

1. 固定 Node/npm 依赖和 generate/typecheck/test/build scripts；
2. 生成 OpenAPI TypeScript types，封装 cookie/CSRF/error client；
3. 实现 tokens、基础 UI primitives、desktop/mobile shell；
4. 实现 bootstrap/login/session/logout；
5. 测试并回写。

## 进展

- 2026-07-22：完成。固定 React/Vite/Tailwind/TanStack 依赖和 lockfile；OpenAPI 生成 client、CSRF/session error boundary、响应式 shell、bootstrap/login/logout 已落地。
- 验证：`npm run generate` 连续 SHA-256 一致；typecheck、3 个 Vitest 文件/5 条断言通过。
