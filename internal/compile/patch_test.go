package compile

import (
	"strings"
	"testing"

	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"

	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// mergePatch / jsonPatch 构造 EnvoyPatch 的便捷函数。
func mergePatch(target, value string) protocol.EnvoyPatch {
	return protocol.EnvoyPatch{Target: target, Op: protocol.PatchOpMerge, Value: protocol.RawJSON(value)}
}

func jsonPatch(target, value string) protocol.EnvoyPatch {
	return protocol.EnvoyPatch{Target: target, Op: protocol.PatchOpJSONPatch, Value: protocol.RawJSON(value)}
}

// compileWithUpstreamPatch 对 base 配置的 Upstream 施加 patch 后编译。
func compileWithUpstreamPatch(t *testing.T, mode Mode, patches ...protocol.EnvoyPatch) (*ir.IR, []CompileError) {
	t.Helper()
	cs := baseHTTPConfig()
	cs.Upstreams[0].Spec.EnvoyPatch = patches
	return Compile(cs, Options{Mode: mode})
}

// assertStageErr 断言恰好一条指定阶段的错误，且 Message 含 substr。
func assertStageErr(t *testing.T, errs []CompileError, stage Stage, substr string) {
	t.Helper()
	if len(errs) != 1 {
		t.Fatalf("want exactly one %s-stage error, got %d:\n%s", stage, len(errs), formatErrs(errs))
	}
	if errs[0].Stage != stage {
		t.Fatalf("Stage = %q, want %q:\n%s", errs[0].Stage, stage, formatErrs(errs))
	}
	if !strings.Contains(errs[0].Message, substr) {
		t.Fatalf("Message = %q, want substring %q", errs[0].Message, substr)
	}
}

// TestBadPatchRejected 坏 patch 反例（编译层 §8.2/§8.5 承诺：
// 「要么编译错误要么产物过校验」，写坏 = 发布被拦截）。
func TestBadPatchRejected(t *testing.T) {
	t.Run("merge with unknown field", func(t *testing.T) {
		out, errs := compileWithUpstreamPatch(t, ModeStatic,
			mergePatch("cluster", `{"nosuch_field": 1}`))
		if out != nil {
			t.Fatal("IR should be nil")
		}
		assertStageErr(t, errs, StagePatch, "round-trip")
		if errs[0].Source.Path != "spec.envoyPatch[0]" {
			t.Fatalf("Source.Path = %q, want spec.envoyPatch[0]", errs[0].Source.Path)
		}
	})

	t.Run("merge with type mismatch", func(t *testing.T) {
		out, errs := compileWithUpstreamPatch(t, ModeStatic,
			mergePatch("cluster", `{"connect_timeout": "abc"}`))
		if out != nil {
			t.Fatal("IR should be nil")
		}
		assertStageErr(t, errs, StagePatch, "round-trip")
	})

	t.Run("jsonPatch on missing path", func(t *testing.T) {
		out, errs := compileWithUpstreamPatch(t, ModeStatic,
			jsonPatch("cluster", `[{"op": "replace", "path": "/no/such/field", "value": 1}]`))
		if out != nil {
			t.Fatal("IR should be nil")
		}
		assertStageErr(t, errs, StagePatch, "apply jsonPatch")
	})

	t.Run("patch yields PGV-invalid resource: caught by F6", func(t *testing.T) {
		// 把 cluster name 替换为空：patch 本身合法，产物非法，须被 F6 拦截。
		out, errs := compileWithUpstreamPatch(t, ModeStatic,
			jsonPatch("cluster", `[{"op": "replace", "path": "/name", "value": ""}]`))
		if out != nil {
			t.Fatal("IR should be nil")
		}
		if len(errs) == 0 || errs[0].Stage != StageValidate {
			t.Fatalf("want validate-stage error, got:\n%s", formatErrs(errs))
		}
	})

	t.Run("patch breaks cross-resource closure: caught by F6", func(t *testing.T) {
		// route 引用 us/app；把 cluster 改名后引用悬空。
		out, errs := compileWithUpstreamPatch(t, ModeStatic,
			jsonPatch("cluster", `[{"op": "replace", "path": "/name", "value": "us/renamed"}]`))
		if out != nil {
			t.Fatal("IR should be nil")
		}
		if len(errs) == 0 || errs[0].Stage != StageValidate {
			t.Fatalf("want validate-stage error, got:\n%s", formatErrs(errs))
		}
	})

	t.Run("invalid target for kind", func(t *testing.T) {
		out, errs := compileWithUpstreamPatch(t, ModeStatic,
			mergePatch("listener", `{}`))
		if out != nil {
			t.Fatal("IR should be nil")
		}
		assertStageErr(t, errs, StagePatch, "invalid target")
		if errs[0].Source.Path != "spec.envoyPatch[0].target" {
			t.Fatalf("Source.Path = %q", errs[0].Source.Path)
		}
	})

	t.Run("gateway has no patchable target", func(t *testing.T) {
		cs := baseHTTPConfig()
		cs.Gateway = &protocol.Gateway{
			APIVersion: protocol.APIVersionV1Alpha1,
			Kind:       protocol.KindGateway,
			Metadata:   protocol.ObjectMeta{Name: protocol.DefaultGatewayName},
			Spec: protocol.GatewaySpec{
				EnvoyPatch: []protocol.EnvoyPatch{mergePatch("bootstrap", `{}`)},
			},
			Origin: testOrigin(),
		}
		out, errs := Compile(cs, Options{Mode: ModeStatic})
		if out != nil {
			t.Fatal("IR should be nil")
		}
		assertStageErr(t, errs, StagePatch, "no patchable target")
	})
}

// TestRuleLevelPatch 覆盖 C1 rule 级定位：target route/<ruleName>。
func TestRuleLevelPatch(t *testing.T) {
	namedRoute := func() *protocol.Route {
		r := newHTTPRoute("api", []string{"http"}, nil, "app")
		r.Spec.Rules[0].Name = "catch-all"
		return r
	}

	t.Run("named rule patched", func(t *testing.T) {
		cs := baseHTTPConfig()
		cs.Routes[0] = namedRoute()
		cs.Routes[0].Spec.EnvoyPatch = []protocol.EnvoyPatch{
			mergePatch("route/catch-all", `{"match": {"prefix": "/patched"}}`),
		}
		out, errs := Compile(cs, Options{Mode: ModeStatic})
		assertNoErrs(t, errs)
		// static 模式 route_config 内联进 HCM；经 listener 找回 vhost。
		rc := inlinedRouteConfig(t, out, "lis/http")
		got := rc.GetVirtualHosts()[0].GetRoutes()[0].GetMatch().GetPrefix()
		if got != "/patched" {
			t.Fatalf("route match prefix = %q, want /patched", got)
		}
	})

	t.Run("bare route target requires rule name", func(t *testing.T) {
		cs := baseHTTPConfig()
		cs.Routes[0] = namedRoute()
		cs.Routes[0].Spec.EnvoyPatch = []protocol.EnvoyPatch{mergePatch("route", `{}`)}
		out, errs := Compile(cs, Options{Mode: ModeStatic})
		if out != nil {
			t.Fatal("IR should be nil")
		}
		assertStageErr(t, errs, StagePatch, "route/<ruleName>")
	})

	t.Run("unnamed rule cannot be addressed", func(t *testing.T) {
		cs := baseHTTPConfig() // newHTTPRoute 的 rule 无 name
		cs.Routes[0].Spec.EnvoyPatch = []protocol.EnvoyPatch{mergePatch("route/ghost", `{}`)}
		out, errs := Compile(cs, Options{Mode: ModeStatic})
		if out != nil {
			t.Fatal("IR should be nil")
		}
		assertStageErr(t, errs, StagePatch, "no rule named")
	})

	t.Run("virtualHost target patched", func(t *testing.T) {
		cs := baseHTTPConfig()
		cs.Routes[0].Spec.EnvoyPatch = []protocol.EnvoyPatch{
			mergePatch("virtualHost", `{"include_request_attempt_count": true}`),
		}
		out, errs := Compile(cs, Options{Mode: ModeXDS})
		assertNoErrs(t, errs)
		vh := out.Routes["rc/http"].GetVirtualHosts()[0]
		if !vh.GetIncludeRequestAttemptCount() {
			t.Fatal("include_request_attempt_count = false, want true (virtualHost patch)")
		}
	})
}

// TestSecretPatch 覆盖 Listener → secret target（C1）：patch 证书 Secret，
// xds 进 IR.Secrets 并被 SDS 引用；static 回写 transport socket。
func TestSecretPatch(t *testing.T) {
	newHTTPSConfig := func(t *testing.T) *protocol.ConfigSet {
		dir := t.TempDir()
		cert, key := writeSelfSignedCert(t, dir, "www", "www.example.com")
		return &protocol.ConfigSet{
			Listeners: []*protocol.Listener{newTLSListener("https", 443, protocol.ProtocolHTTPS, cert, key)},
			Routes:    []*protocol.Route{newHTTPRoute("www", []string{"https"}, []string{"www.example.com"}, "app")},
			Upstreams: []*protocol.Upstream{newUpstream("app")},
		}
	}
	secretPatch := mergePatch("secret",
		`{"tls_certificate": {"password": {"filename": "/etc/esgw/key.pass"}}}`)

	t.Run("xds: patched secret in IR.Secrets with SDS reference", func(t *testing.T) {
		cs := newHTTPSConfig(t)
		cs.Listeners[0].Spec.EnvoyPatch = []protocol.EnvoyPatch{secretPatch}
		out, errs := Compile(cs, Options{Mode: ModeXDS})
		assertNoErrs(t, errs)
		sec := out.Secrets["crt/https/0"]
		if sec == nil {
			t.Fatal("secret crt/https/0 missing in xds IR")
		}
		if got := sec.GetTlsCertificate().GetPassword().GetFilename(); got != "/etc/esgw/key.pass" {
			t.Fatalf("password.filename = %q (secret patch)", got)
		}
		lis := out.Listeners["lis/https"]
		down, _ := decodeDownstreamTLS(lis.GetFilterChains()[0])
		sds := down.GetCommonTlsContext().GetTlsCertificateSdsSecretConfigs()
		if len(sds) != 1 || sds[0].GetName() != "crt/https/0" {
			t.Fatalf("SDS ref = %v, want crt/https/0", sds)
		}
		if len(down.GetCommonTlsContext().GetTlsCertificates()) != 0 {
			t.Fatal("xds: inline certificates should be replaced by SDS ref")
		}
	})

	t.Run("static: patched secret written back inline", func(t *testing.T) {
		cs := newHTTPSConfig(t)
		cs.Listeners[0].Spec.EnvoyPatch = []protocol.EnvoyPatch{secretPatch}
		out, errs := Compile(cs, Options{Mode: ModeStatic})
		assertNoErrs(t, errs)
		if len(out.Secrets) != 0 {
			t.Fatal("static: compiled secrets should be inlined back")
		}
		lis := out.Listeners["lis/https"]
		down, _ := decodeDownstreamTLS(lis.GetFilterChains()[0])
		tcs := down.GetCommonTlsContext().GetTlsCertificates()
		if len(tcs) != 1 || tcs[0].GetPassword().GetFilename() != "/etc/esgw/key.pass" {
			t.Fatalf("static write-back lost secret patch: %+v", tcs)
		}
	})

	t.Run("secret target on cleartext listener", func(t *testing.T) {
		cs := baseHTTPConfig()
		cs.Listeners[0].Spec.EnvoyPatch = []protocol.EnvoyPatch{secretPatch}
		out, errs := Compile(cs, Options{Mode: ModeStatic})
		if out != nil {
			t.Fatal("IR should be nil")
		}
		assertStageErr(t, errs, StagePatch, "has no certificates")
	})
}

// inlinedRouteConfig 从 static 形态 Listener 的 HCM 中取内联 route_config。
func inlinedRouteConfig(t *testing.T, out *ir.IR, lisName string) *routev3.RouteConfiguration {
	t.Helper()
	lis := out.Listeners[lisName]
	if lis == nil {
		t.Fatalf("listener %q missing", lisName)
	}
	hcm := decodeHCM(findHCMFilter(lis))
	if hcm == nil || hcm.GetRouteConfig() == nil {
		t.Fatalf("listener %q has no inlined route_config", lisName)
	}
	return hcm.GetRouteConfig()
}
