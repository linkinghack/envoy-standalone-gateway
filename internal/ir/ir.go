package ir

// IR 是编译产物：一份完整、自洽、已通过校验的 Envoy v3 资源集合（编译层 §1）。
// 完整定义（Listeners/Clusters/Routes/Endpoints/Secrets/Bootstrap、Version、SourceMap）
// 在 Sprint 260719 T5 落地；此处为满足 Compile() 冻结签名的最小占位。
type IR struct{}
