// Package protocol 实现网关配置抽象协议 v1alpha1（设计文档 260716-1）：
// 资源信封与五对象类型、strict decode、目录加载（Origin）、defaults、JSON Schema。
//
// 本包只处理单文档结构层；跨对象语义校验属 internal/compile（F2）。
package protocol
