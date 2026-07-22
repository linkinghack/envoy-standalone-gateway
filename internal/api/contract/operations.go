// Package contract defines the management HTTP operation registry shared by
// contract tests and the standard-library router adapter.
package contract

// Operation is one OpenAPI operation exposed by the management server.
type Operation struct {
	ID        string
	Method    string
	Path      string
	Anonymous bool
}

// Operations is the complete S5 operation surface. The contract test proves
// this registry and api/openapi.yaml remain one-to-one.
var Operations = []Operation{
	{ID: "getAuthBootstrap", Method: "GET", Path: "/api/v1/auth/bootstrap", Anonymous: true},
	{ID: "createAuthBootstrap", Method: "POST", Path: "/api/v1/auth/bootstrap", Anonymous: true},
	{ID: "login", Method: "POST", Path: "/api/v1/auth/login", Anonymous: true},
	{ID: "logout", Method: "POST", Path: "/api/v1/auth/logout"},
	{ID: "getAuthSession", Method: "GET", Path: "/api/v1/auth/session"},
	{ID: "changePassword", Method: "POST", Path: "/api/v1/auth/password"},
	{ID: "listAuthMethods", Method: "GET", Path: "/api/v1/auth/methods", Anonymous: true},
	{ID: "getConfigDraft", Method: "GET", Path: "/api/v1/config/draft"},
	{ID: "replaceConfigDraft", Method: "PUT", Path: "/api/v1/config/draft"},
	{ID: "listConfigObjects", Method: "GET", Path: "/api/v1/config/objects"},
	{ID: "getConfigObject", Method: "GET", Path: "/api/v1/config/objects/{kind}/{name}"},
	{ID: "putConfigObject", Method: "PUT", Path: "/api/v1/config/objects/{kind}/{name}"},
	{ID: "deleteConfigObject", Method: "DELETE", Path: "/api/v1/config/objects/{kind}/{name}"},
	{ID: "getConfigSchemas", Method: "GET", Path: "/api/v1/config/schemas"},
	{ID: "validateConfig", Method: "POST", Path: "/api/v1/config/validate"},
	{ID: "getCompiledConfig", Method: "GET", Path: "/api/v1/config/compiled"},
	{ID: "getDraftDiff", Method: "GET", Path: "/api/v1/config/draft/diff"},
	{ID: "publishConfig", Method: "POST", Path: "/api/v1/config/publish"},
	{ID: "getConfigStatus", Method: "GET", Path: "/api/v1/config/status"},
	{ID: "listConfigVersions", Method: "GET", Path: "/api/v1/config/versions"},
	{ID: "getConfigVersion", Method: "GET", Path: "/api/v1/config/versions/{id}"},
	{ID: "getConfigVersionSource", Method: "GET", Path: "/api/v1/config/versions/{id}/config"},
	{ID: "getConfigVersionCompiled", Method: "GET", Path: "/api/v1/config/versions/{id}/compiled"},
	{ID: "getConfigVersionDiff", Method: "GET", Path: "/api/v1/config/versions/{id}/diff"},
	{ID: "rollbackConfigVersion", Method: "POST", Path: "/api/v1/config/versions/{id}/rollback"},
	{ID: "getStatusSummary", Method: "GET", Path: "/api/v1/status/summary"},
	{ID: "listStatusListeners", Method: "GET", Path: "/api/v1/status/listeners"},
	{ID: "listStatusClusters", Method: "GET", Path: "/api/v1/status/clusters"},
	{ID: "listStatusClusterEndpoints", Method: "GET", Path: "/api/v1/status/clusters/{name}/endpoints"},
	{ID: "listStatusRoutes", Method: "GET", Path: "/api/v1/status/routes"},
	{ID: "listStatusCerts", Method: "GET", Path: "/api/v1/status/certs"},
	{ID: "getStatsOverview", Method: "GET", Path: "/api/v1/stats/overview"},
	{ID: "listStatsSeries", Method: "GET", Path: "/api/v1/stats/series"},
	{ID: "listCertificates", Method: "GET", Path: "/api/v1/certs"},
	{ID: "createCertificate", Method: "POST", Path: "/api/v1/certs"},
	{ID: "getCertificate", Method: "GET", Path: "/api/v1/certs/{name}"},
	{ID: "deleteCertificate", Method: "DELETE", Path: "/api/v1/certs/{name}"},
	{ID: "getSystemInfo", Method: "GET", Path: "/api/v1/system/info"},
	{ID: "getEnvoyPrometheusStats", Method: "GET", Path: "/api/v1/envoy/stats/prometheus"},
	{ID: "getHealth", Method: "GET", Path: "/healthz", Anonymous: true},
	{ID: "getReady", Method: "GET", Path: "/readyz", Anonymous: true},
	{ID: "getManagementMetrics", Method: "GET", Path: "/metrics", Anonymous: true},
}
