package compile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strings"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	extauthzv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_authz/v3"
	upstreamhttpv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// extAuthAssembly collects the one listener-scoped ext_authz provider. Envoy's
// service and fail-open settings live on the HCM filter, so different effective
// providers on rules sharing a Listener are rejected instead of silently choosing one.
type extAuthAssembly struct {
	key     string
	config  *extauthzv3.ExtAuthz
	cluster string
}

func newExtAuthAssembly() *extAuthAssembly { return &extAuthAssembly{} }
func (a *extAuthAssembly) enabled() bool   { return a != nil && a.config != nil }

func (ctx *buildContext) registerExtAuth(asm *extAuthAssembly, p *protocol.ExtAuthPolicy) error {
	if p == nil || p.Disabled {
		return nil
	}
	key, err := extAuthProviderKey(p)
	if err != nil {
		return err
	}
	if asm.enabled() {
		if asm.key != key {
			return fmt.Errorf("extAuth provider conflicts with another effective policy on this listener")
		}
		return nil
	}
	cfg, cluster, err := ctx.buildExtAuthProvider(p, key)
	if err != nil {
		return err
	}
	asm.key, asm.config, asm.cluster = key, cfg, cluster
	return nil
}

func extAuthProviderKey(p *protocol.ExtAuthPolicy) (string, error) {
	var service string
	switch {
	case p.GRPC != nil:
		host, port, err := net.SplitHostPort(p.GRPC.Address)
		if err != nil {
			return "", fmt.Errorf("invalid extAuth gRPC address %q: %v", p.GRPC.Address, err)
		}
		service = "grpc|" + net.JoinHostPort(strings.ToLower(host), port)
	case p.HTTP != nil:
		u, err := url.Parse(p.HTTP.Address)
		if err != nil {
			return "", fmt.Errorf("invalid extAuth HTTP address %q: %v", p.HTTP.Address, err)
		}
		service = fmt.Sprintf("http|%s|%s|%s|ca=%s|insecure=%t",
			strings.ToLower(u.Scheme), net.JoinHostPort(strings.ToLower(u.Hostname()), u.Port()),
			p.HTTP.PathPrefix, p.HTTP.CAFile, p.HTTP.InsecureSkipVerify)
	default:
		return "", fmt.Errorf("extAuth provider is missing grpc/http service")
	}
	return fmt.Sprintf("%s|failOpen=%t", service, p.FailOpen), nil
}

func extAuthClusterName(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "extauth/" + hex.EncodeToString(sum[:6])
}

func (ctx *buildContext) buildExtAuthProvider(p *protocol.ExtAuthPolicy, key string) (*extauthzv3.ExtAuthz, string, error) {
	name := extAuthClusterName(key)
	cfg := &extauthzv3.ExtAuthz{
		TransportApiVersion: corev3.ApiVersion_V3,
		FailureModeAllow:    p.FailOpen,
	}
	var cluster *clusterv3.Cluster
	var err error
	switch {
	case p.GRPC != nil:
		cluster, err = buildExtAuthCluster(name, p.GRPC.Address, nil, true)
		cfg.Services = &extauthzv3.ExtAuthz_GrpcService{GrpcService: &corev3.GrpcService{
			TargetSpecifier: &corev3.GrpcService_EnvoyGrpc_{
				EnvoyGrpc: &corev3.GrpcService_EnvoyGrpc{ClusterName: name},
			},
			Timeout: durationpb.New(protocol.DefaultConnectTimeout),
		}}
	case p.HTTP != nil:
		u, parseErr := url.Parse(p.HTTP.Address)
		if parseErr != nil {
			return nil, "", parseErr
		}
		var tlsConfig *protocol.ExtAuthHTTP
		if u.Scheme == "https" {
			tlsConfig = p.HTTP
		}
		cluster, err = buildExtAuthCluster(name, u.Host, tlsConfig, false)
		cfg.Services = &extauthzv3.ExtAuthz_HttpService{HttpService: &extauthzv3.HttpService{
			ServerUri: &corev3.HttpUri{
				Uri:              p.HTTP.Address,
				HttpUpstreamType: &corev3.HttpUri_Cluster{Cluster: name},
				Timeout:          durationpb.New(protocol.DefaultConnectTimeout),
			},
			PathPrefix: p.HTTP.PathPrefix,
		}}
	}
	if err != nil {
		return nil, "", err
	}
	ctx.extAuth[name] = cluster
	return cfg, name, nil
}

func buildExtAuthCluster(name, address string, httpTLS *protocol.ExtAuthHTTP, http2 bool) (*clusterv3.Cluster, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid extAuth address %q: %v", address, err)
	}
	var portNum int
	if _, err := fmt.Sscanf(port, "%d", &portNum); err != nil {
		return nil, fmt.Errorf("invalid extAuth port %q", port)
	}
	typ := clusterv3.Cluster_STRICT_DNS
	if net.ParseIP(host) != nil {
		typ = clusterv3.Cluster_STATIC
	}
	cluster := &clusterv3.Cluster{
		Name:                 name,
		ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: typ},
		ConnectTimeout:       durationpb.New(protocol.DefaultConnectTimeout),
		LoadAssignment: &endpointv3.ClusterLoadAssignment{
			ClusterName: name,
			Endpoints: []*endpointv3.LocalityLbEndpoints{{
				LbEndpoints: []*endpointv3.LbEndpoint{lbEndpoint(host, int32(portNum), nil)},
			}},
		},
	}
	if httpTLS != nil {
		cluster.TransportSocket, err = upstreamTLSSocket(&protocol.UpstreamTLS{
			Enabled:            true,
			SNI:                host,
			CAFile:             httpTLS.CAFile,
			InsecureSkipVerify: httpTLS.InsecureSkipVerify,
		}, nil)
		if err != nil {
			return nil, err
		}
	}
	if http2 {
		opts, err := marshalAny(&upstreamhttpv3.HttpProtocolOptions{
			UpstreamProtocolOptions: &upstreamhttpv3.HttpProtocolOptions_ExplicitHttpConfig_{
				ExplicitHttpConfig: &upstreamhttpv3.HttpProtocolOptions_ExplicitHttpConfig{
					ProtocolConfig: &upstreamhttpv3.HttpProtocolOptions_ExplicitHttpConfig_Http2ProtocolOptions{
						Http2ProtocolOptions: &corev3.Http2ProtocolOptions{},
					},
				},
			},
		})
		if err != nil {
			return nil, err
		}
		cluster.TypedExtensionProtocolOptions = map[string]*anypb.Any{
			"envoy.extensions.upstreams.http.v3.HttpProtocolOptions": opts,
		}
	}
	return cluster, nil
}

func finalizeExtAuthRoutes(rc *routev3.RouteConfiguration, enabled bool) error {
	disabled, err := marshalAny(&extauthzv3.ExtAuthzPerRoute{
		Override: &extauthzv3.ExtAuthzPerRoute_Disabled{Disabled: true},
	})
	if err != nil {
		return err
	}
	for _, vh := range rc.GetVirtualHosts() {
		for _, route := range vh.GetRoutes() {
			if !enabled {
				delete(route.TypedPerFilterConfig, extAuthzFilterName)
				if len(route.TypedPerFilterConfig) == 0 {
					route.TypedPerFilterConfig = nil
				}
				continue
			}
			if route.GetTypedPerFilterConfig()[extAuthzFilterName] == nil {
				if route.TypedPerFilterConfig == nil {
					route.TypedPerFilterConfig = map[string]*anypb.Any{}
				}
				route.TypedPerFilterConfig[extAuthzFilterName] = disabled
			}
		}
	}
	return nil
}
