# Sprint 260724 需求：P0 专业管理控制台

## 目标

落地路线图 S6：提供随单一 `esgw` 二进制交付的响应式同源管理控制台，使 P0 用户无需命令行即可完成首次引导、配置编辑、校验发布、运行状态查看和证书管理。

## 范围

- React + TypeScript + Vite 工程、shadcn/ui 源码组件体系、design tokens；
- OpenAPI 生成 TypeScript 类型与统一同源 API client；
- 首次引导、登录、session 恢复和退出；
- Dashboard、五对象配置浏览、YAML 编辑、校验/发布闭环；
- listeners/clusters/endpoints/routes/certs 运行状态；
- 托管证书、编译产物/专家视图、系统信息；
- loading/empty/error/stale 状态、键盘可达、窄屏布局；
- 构建产物 Go embed、前端测试和浏览器 smoke。

## 非范围

- M2 完整统计、版本历史/回滚增强 UI；
- OIDC、多用户/RBAC；
- Envoy 进程托管和 static hot restart（S7）；
- 插件市场、k8s disco 和 Helm。

## 验收标准

1. bootstrap/login/session/logout 可用，401 自动回登录且不形成刷新循环；
2. 配置对象可浏览，YAML 可读取/编辑并保留服务端 `resourceVersion` 并发保护；
3. UI 可执行 validate→review→publish→status，错误能定位且不被成功态覆盖；
4. 可查看归一化运行状态、托管证书、编译产物和系统信息；
5. 主路径键盘可完成，窄屏无关键操作丢失，颜色不是状态唯一表达；
6. OpenAPI TypeScript 类型重复生成无 diff；
7. `npm run typecheck test build`、Go embed test、全仓 Go 门禁通过。
