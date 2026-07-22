# Sprint 260724 任务拆分与进展追踪

| ID | 任务 | 状态 | 验收 |
|---|---|---|---|
| T1 | web 基线、tokens、shell、OpenAPI client、auth | 已完成 | generate/typecheck/unit/build |
| T2 | Overview 与 Configuration 编辑 | 已完成 | draft/object/editor tests |
| T3 | validate/publish/status 与 Runtime | 已完成 | publish flow/state tests |
| T4 | Certificates、Expert、System | 已完成 | API mutation/read tests |
| T5 | accessibility/窄屏、Go embed、browser smoke、收口 | 已完成 | frontend + Go 全门禁 |

## 冲刺日志

| 日期 | 事项 |
|---|---|
| 2026-07-22 | S5 A1~A7 收口并提交；读取 frontend-design 技能，冻结“运维信号台”视觉方向，创建 S6 文档结构，T1 开始 |
| 2026-07-22 | T1~T4 完成：OpenAPI client/auth、六个主页面、完整 YAML 编辑、发布流、状态与证书工作流落地 |
| 2026-07-22 | Vite 7 + 本地 Monaco/yaml worker 生产构建；Go embed 默认接入唯一组合根，SPA 深链通过 |
| 2026-07-22 | 实机审计桌面与 390px 移动布局；前端全门禁及 Go build/test/race/vet/lint 通过，S6 收口 |

## 验收核验

| ID | 状态 | 证据 |
|---|---|---|
| A1 auth/session | 通过 | AuthBoundary unit + Playwright bootstrap/session |
| A2 config/publish | 通过 | 完整 draft、resourceVersion、validate/review/publish UI 与 typed API |
| A3 runtime/certs/expert/system | 通过 | 各领域 loading/empty/error/read/mutation 页面 |
| A4 accessibility/responsive | 通过 | 语义 label/focus/reduced-motion；Chromium desktop/mobile smoke |
| A5 OpenAPI/client 一致 | 通过 | `generated.ts` 连续生成 SHA-256 均为 `0abb1424f4bb64e1967e1b1bb8f8b0cc2ed7d95041df97ee5845b7a1324ad90f` |
| A6 embed/SPA routing | 通过 | `internal/console` embed test；core `/configuration` 深链测试 |
| A7 工程质量门禁 | 通过 | `npm run check`；`make build`；Go test/race/vet；golangci-lint 0 issues |
