// Package config 实现 esgw.yaml（esgw 进程自身配置）的加载、默认值与校验。
//
// 与 internal/protocol（网关配置协议对象）不同，本包处理管理面进程配置：
// 单文档、strict decode（未知字段报错，SD4）。schema 真源见
// design_docs/dev_manage/dev_design/260720-1-mcore-assembly.md §3。
package config
