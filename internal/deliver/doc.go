// Package deliver 是下发层（M-DELIVER）根包：Deliverer 接口与状态/事件
// 模型（deliver.go，下发层 §6）、xds 模式的 snapshot protojson 导出
// （SnapshotJSON，esgw compile --mode xds 的产物）。ADS server 在
// 子包 xds，static 渲染在子包 static。
package deliver
