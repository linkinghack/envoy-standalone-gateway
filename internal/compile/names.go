package compile

import "strconv"

// 资源命名规约（编译层 §5，硬约束）：
// lis/<listener>、rc/<listener>、vh/<route>、us/<upstream>、crt/<listener>/<n>。
// 「/」分隔避免与用户 name 字符集冲突；前缀短因其出现在 stats 名中
// （M-STATE 反解归属靠它：http.lis/<n>.*、cluster 维度 us/<n>）。
// crt/<listener>/<n>（证书 Secret，xDS 形态 SDS）由 T5 形态化落地时引入。

// listenerResourceName 返回 Listener 资源名（兼作 HCM stat_prefix，编译层 §5）。
func listenerResourceName(name string) string { return "lis/" + name }

// routeConfigName 返回 RouteConfiguration 资源名（HCM RDS 引用同名）。
func routeConfigName(listener string) string { return "rc/" + listener }

// virtualHostName 返回 VirtualHost 名（每 Route 对象一个）。
func virtualHostName(route string) string { return "vh/" + route }

// clusterResourceName 返回 Cluster 资源名。
func clusterResourceName(upstream string) string { return "us/" + upstream }

// secretResourceName 返回证书 Secret 资源名（xDS 形态 SDS 引用同名）。
func secretResourceName(listener string, n int) string {
	return "crt/" + listener + "/" + strconv.Itoa(n)
}

// Envoy 标准 filter / transport socket / access logger 名。
// HTTP filter 名同时是 typed_per_filter_config 的 key（编译层 §3 策略表）。
const (
	hcmFilterName            = "envoy.filters.network.http_connection_manager"
	rbacFilterName           = "envoy.filters.http.rbac"
	tcpProxyFilterName       = "envoy.filters.network.tcp_proxy"
	udpProxyFilterName       = "envoy.filters.udp_listener.udp_proxy"
	corsFilterName           = "envoy.filters.http.cors"
	jwtAuthnFilterName       = "envoy.filters.http.jwt_authn"
	extAuthzFilterName       = "envoy.filters.http.ext_authz"
	localRateLimitFilterName = "envoy.filters.http.local_ratelimit"
	routerFilterName         = "envoy.filters.http.router"
	tlsTransportSocketName   = "envoy.transport_sockets.tls"
	tlsInspectorFilterName   = "envoy.filters.listener.tls_inspector"
	fileAccessLogName        = "envoy.access_loggers.file"
)
