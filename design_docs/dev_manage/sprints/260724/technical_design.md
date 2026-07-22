# Sprint 260724 技术设计

## 1. 视觉与信息架构

采用“运维信号台”方向：深墨色固定导航承载全局上下文，暖灰工作区承载高密度数据，橙色只用于变更/警告，青绿色用于健康信号。标题使用 Newsreader，正文使用 IBM Plex Sans，代码/版本使用 IBM Plex Mono；避免通用 SaaS 蓝紫渐变和纯卡片堆叠。

桌面以 12 列工作区组织，运行信号用贯穿页面的 status rail 聚合；窄屏导航收为顶部和抽屉，关键发布动作固定在可见操作区。页面为：Overview、Configuration、Runtime、Certificates、Expert、System；登录/引导为独立入口。

## 2. 工程边界

- `web/src/api/generated.ts`：由 OpenAPI 固定工具生成的类型真源；
- `web/src/api/client.ts`：同源 fetch、session cookie、CSRF、稳定 error；
- `web/src/features/*`：领域查询/变更和页面，不把服务端状态塞入通用 UI 组件；
- `web/src/components/ui/*`：shadcn/ui 风格的源码组件，仅含视觉与可访问性语义；
- `web/src/styles/*`：design tokens、基础排版和布局；
- `web/dist`：生产构建产物，复制到 Go embed package，不手工修改。

## 3. 数据与错误流

TanStack Query 管理 session、config、state 和 certs。401 由 client 抛出稳定 `APIError`，AuthBoundary 只做一次 session 查询并跳转；mutation 成功后按领域键失效，不做全局刷新。所有写请求固定发送 `X-ESGW-Request: 1`。

YAML 编辑以完整 draft 文件为 P0 安全真源；保存携带最后读取的 `resourceVersion`。Monaco 延迟加载，页面先显示 skeleton；无法初始化时提供 textarea 降级而不阻塞编辑。

## 4. 构建与嵌入

Vite `base=/` 生成 hash assets。Go `embed.FS` 只嵌入构建后同步到 `internal/console/dist` 的文件；同步脚本先校验 `index.html` 和 assets manifest，再替换目录。开发环境由 Vite proxy `/api`、`/healthz`、`/readyz`、`/metrics` 到本机 esgw。

生产组合根把 embed FS 传入 API server；`index.html` no-cache、hash assets immutable 的策略沿用 S5 测试。

## 5. 测试策略

Vitest + Testing Library 覆盖 auth boundary、API error、publish flow 状态和关键组件；Playwright smoke 覆盖引导→配置→发布→状态。无浏览器依赖时至少执行 build 后静态路由/asset embed 测试，浏览器门禁必须如实标注。
