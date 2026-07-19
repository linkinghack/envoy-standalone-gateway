// Package compile 实现编译流水线 F1~F6（设计文档 260716-2）：
// 链接与语义校验、Builder、escape hatch 合成、形态化、PGV 校验。
//
// Compile() 保持纯函数：本包不 import envoycheck 子包与任何 IO（SD5）。
package compile
