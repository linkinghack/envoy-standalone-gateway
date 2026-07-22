package compile

import (
	"strings"
	"testing"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// TestL4ClusterReferenceValidation ensures F6 checks cluster references hidden in
// network and UDP listener filter typed configs, not only HTTP routes.
func TestL4ClusterReferenceValidation(t *testing.T) {
	tests := []struct {
		name     string
		protocol protocol.ListenerProtocol
		port     int32
		want     string
	}{
		{name: "tcp", protocol: protocol.ProtocolTCP, port: 3306, want: "TCP proxy"},
		{name: "udp", protocol: protocol.ProtocolUDP, port: 5353, want: "UDP proxy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := &protocol.ConfigSet{
				Listeners: []*protocol.Listener{newListener(tt.name, tt.port, tt.protocol)},
				Routes:    []*protocol.Route{newForwardRoute(tt.name, []string{tt.name}, "backend")},
				Upstreams: []*protocol.Upstream{newUpstream("backend")},
			}
			out, errs := Compile(cs, Options{Mode: ModeStatic})
			assertNoErrs(t, errs)
			delete(out.Clusters, "us/backend")
			verrs := validateIR(out)
			if len(verrs) != 1 || verrs[0].Stage != StageValidate ||
				!strings.Contains(verrs[0].Message, tt.want) ||
				!strings.Contains(verrs[0].Message, `"us/backend"`) {
				t.Fatalf("want one %s missing-cluster validation error, got:\n%s", tt.want, formatErrs(verrs))
			}
			if verrs[0].Source.Kind != protocol.KindListener || verrs[0].Source.Name != tt.name {
				t.Fatalf("error source = %#v, want Listener/%s", verrs[0].Source, tt.name)
			}
		})
	}
}

func TestExtAuthClusterReferenceValidation(t *testing.T) {
	spec := protocol.PolicySpec{ExtAuth: &protocol.ExtAuthPolicy{
		GRPC: &protocol.ExtAuthGRPC{Address: "127.0.0.1:9000"},
	}}
	cs := policyCS(nil, nil, nil, []protocol.PolicyAttachment{{Inline: &spec}})
	out, errs := Compile(cs, Options{Mode: ModeStatic})
	assertNoErrs(t, errs)
	cluster := ""
	for name := range out.Clusters {
		if strings.HasPrefix(name, "extauth/") {
			cluster = name
			delete(out.Clusters, name)
		}
	}
	if cluster == "" {
		t.Fatal("generated extAuth cluster not found")
	}
	verrs := validateIR(out)
	if len(verrs) != 1 || verrs[0].Stage != StageValidate ||
		!strings.Contains(verrs[0].Message, "ext_authz reference") ||
		!strings.Contains(verrs[0].Message, cluster) {
		t.Fatalf("want one extAuth missing-cluster error, got:\n%s", formatErrs(verrs))
	}
}
