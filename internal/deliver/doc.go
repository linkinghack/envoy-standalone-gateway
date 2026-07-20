// Package deliver 是下发层（M-DELIVER）根包。M0 只含 xds 模式的
// snapshot protojson 导出（SnapshotJSON，esgw compile --mode xds 的产物）；
// ADS server、Deliverer 接口等运行期组件属 S2/M1。
package deliver
