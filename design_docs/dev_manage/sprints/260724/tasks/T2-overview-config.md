# T2：Overview 与 Configuration

## 目标

实现首页运行摘要、配置对象导航和安全的完整 draft YAML 编辑。

## 步骤

1. Overview signal rail 和资源摘要；
2. 五对象筛选/列表；
3. draft 文件导航和 Monaco/textarea 降级编辑；
4. resourceVersion 保存冲突反馈；
5. unit tests。

## 进展

- 2026-07-22：完成。Overview 运行信号轨、完整 draft 文件导航、对象类型索引、Monaco YAML 编辑和 `resourceVersion` 保存保护已落地。
- P0 以完整 YAML 为配置真源；对象索引用于快速确认五类资源，不在浏览器重建服务端 schema。
