package compile

import (
	"sort"
	"strings"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	tlsinspectorv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/tls_inspector/v3"
	tcpproxyv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	udpproxyv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/udp/udp_proxy/v3"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// buildL4Listener 把已通过 F2 约束校验的 TCP/TLS/UDP Listener 编译为 Envoy
// network/UDP listener filter。L4 Route 不生成 RouteConfiguration。
func (ctx *buildContext) buildL4Listener(l *protocol.Listener) (*listenerv3.Listener, []CompileError) {
	routes := ctx.forwardRoutesFor(l.Metadata.Name)
	switch l.Spec.Protocol {
	case protocol.ProtocolTCP:
		return buildTCPListener(l, routes[0])
	case protocol.ProtocolTLS:
		return buildTLSPassthroughListener(l, routes)
	case protocol.ProtocolUDP:
		return buildUDPListener(l, routes[0])
	default:
		return nil, []CompileError{buildError(l.Origin, protocol.KindListener, l.Metadata.Name,
			"spec.protocol", "unsupported L4 protocol %s", l.Spec.Protocol)}
	}
}

// forwardRoutesFor 返回挂到 listener 的合法 forward Route；F2 已确保数量与协议匹配。
func (ctx *buildContext) forwardRoutesFor(listener string) []*protocol.Route {
	var routes []*protocol.Route
	for _, r := range sortedRoutes(ctx.cs.Routes) {
		if r.Spec.Forward != nil && len(r.Spec.Rules) == 0 && attachesTo(r, listener) {
			routes = append(routes, r)
		}
	}
	return routes
}

func buildTCPListener(l *protocol.Listener, r *protocol.Route) (*listenerv3.Listener, []CompileError) {
	filter, buildErr := tcpProxyFilter(l, r)
	if buildErr != nil {
		return nil, []CompileError{*buildErr}
	}
	return &listenerv3.Listener{
		Name:    listenerResourceName(l.Metadata.Name),
		Address: socketAddress(l.Spec.Address, l.Spec.Port),
		FilterChains: []*listenerv3.FilterChain{{
			Filters: []*listenerv3.Filter{filter},
		}},
	}, nil
}

func buildTLSPassthroughListener(l *protocol.Listener, routes []*protocol.Route) (*listenerv3.Listener, []CompileError) {
	type chainSpec struct {
		route *protocol.Route
		hosts []string
	}
	specs := make([]chainSpec, 0, len(routes))
	for _, r := range routes {
		hosts := make([]string, 0, len(r.Spec.Forward.SNIHosts))
		for _, host := range r.Spec.Forward.SNIHosts {
			hosts = append(hosts, strings.ToLower(strings.TrimSpace(host)))
		}
		sort.Strings(hosts)
		specs = append(specs, chainSpec{route: r, hosts: hosts})
	}
	sort.Slice(specs, func(i, j int) bool {
		left, right := strings.Join(specs[i].hosts, "\x00"), strings.Join(specs[j].hosts, "\x00")
		if left != right {
			return left < right
		}
		return specs[i].route.Metadata.Name < specs[j].route.Metadata.Name
	})

	chains := make([]*listenerv3.FilterChain, 0, len(specs))
	for _, spec := range specs {
		filter, buildErr := tcpProxyFilter(l, spec.route)
		if buildErr != nil {
			return nil, []CompileError{*buildErr}
		}
		chains = append(chains, &listenerv3.FilterChain{
			FilterChainMatch: &listenerv3.FilterChainMatch{
				ServerNames:       spec.hosts,
				TransportProtocol: "tls",
			},
			Filters: []*listenerv3.Filter{filter},
		})
	}

	inspector, err := marshalAny(&tlsinspectorv3.TlsInspector{})
	if err != nil {
		return nil, []CompileError{buildError(l.Origin, protocol.KindListener, l.Metadata.Name,
			"", "marshal TlsInspector: %v", err)}
	}
	return &listenerv3.Listener{
		Name:         listenerResourceName(l.Metadata.Name),
		Address:      socketAddress(l.Spec.Address, l.Spec.Port),
		FilterChains: chains,
		ListenerFilters: []*listenerv3.ListenerFilter{{
			Name:       tlsInspectorFilterName,
			ConfigType: &listenerv3.ListenerFilter_TypedConfig{TypedConfig: inspector},
		}},
	}, nil
}

func tcpProxyFilter(l *protocol.Listener, r *protocol.Route) (*listenerv3.Filter, *CompileError) {
	cfg, err := marshalAny(&tcpproxyv3.TcpProxy{
		StatPrefix: listenerResourceName(l.Metadata.Name),
		ClusterSpecifier: &tcpproxyv3.TcpProxy_Cluster{
			Cluster: clusterResourceName(r.Spec.Forward.Upstream),
		},
	})
	if err != nil {
		buildErr := buildError(r.Origin, protocol.KindRoute, r.Metadata.Name,
			"spec.forward", "marshal TcpProxy: %v", err)
		return nil, &buildErr
	}
	return &listenerv3.Filter{
		Name:       tcpProxyFilterName,
		ConfigType: &listenerv3.Filter_TypedConfig{TypedConfig: cfg},
	}, nil
}

func buildUDPListener(l *protocol.Listener, r *protocol.Route) (*listenerv3.Listener, []CompileError) {
	cfg, err := marshalAny(&udpproxyv3.UdpProxyConfig{
		StatPrefix: listenerResourceName(l.Metadata.Name),
		RouteSpecifier: &udpproxyv3.UdpProxyConfig_Cluster{
			Cluster: clusterResourceName(r.Spec.Forward.Upstream),
		},
	})
	if err != nil {
		return nil, []CompileError{buildError(r.Origin, protocol.KindRoute, r.Metadata.Name,
			"spec.forward", "marshal UdpProxyConfig: %v", err)}
	}
	address := socketAddress(l.Spec.Address, l.Spec.Port)
	address.GetSocketAddress().Protocol = corev3.SocketAddress_UDP
	return &listenerv3.Listener{
		Name:              listenerResourceName(l.Metadata.Name),
		Address:           address,
		UdpListenerConfig: &listenerv3.UdpListenerConfig{},
		ListenerFilters: []*listenerv3.ListenerFilter{{
			Name:       udpProxyFilterName,
			ConfigType: &listenerv3.ListenerFilter_TypedConfig{TypedConfig: cfg},
		}},
	}, nil
}
