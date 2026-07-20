# Sprint 260721 需求：配置域与存储基础闭环

## 目标

落地 S3 的第一阶段基础能力：

1. M-STORE 使用 SQLite 保存运行态索引与设置；
2. M-CONF 从 `config.d/` / `native.yaml` 加载草稿，计算确定性 `draftHash`；
3. 发布前将真源文件原样复制为不可变版本快照，并写入版本元数据。

## 范围

- SQLite migration、`settings`、`versions`、`publish_runs` 基础表；
- 抽象协议草稿加载（复用 `protocol.LoadDir`）；
- `config.d` 与 `native.yaml` 互斥检测；
- 文件内容哈希（路径排序、内容逐字节参与）；
- 版本序号分配、快照原子落盘、`meta.json`；
- 单元测试与 `make build test lint`。

## 非范围

- REST API、登录鉴权；
- fsnotify 外部修改监听；
- diff、回滚、完整发布状态机；
- 原生 `native.yaml` 到 IR 的编译器实现。

## 验收标准

- 空数据目录可初始化 SQLite，重复初始化幂等；
- settings 可读写；版本序号单调递增；
- `config.d` 与 `native.yaml` 同时存在时加载失败；
- 相同真源内容在不同调用中产生相同 hash，文件内容或相对路径变化会改变 hash；
- 快照目录采用临时目录 + rename，包含真源副本和可解析 `meta.json`；
- 测试、构建、lint 全绿。
