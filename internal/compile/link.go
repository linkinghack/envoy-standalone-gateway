package compile

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// linked 是 F2 引用解析的结果：全部 name 引用建立的指针索引，
// 供 F3 构建阶段消费（T4）。
type linked struct {
	listeners map[string]*protocol.Listener
	upstreams map[string]*protocol.Upstream
	policies  map[string]*protocol.Policy
}

// link 执行编译流水线 F2：默认值填充、引用解析与跨对象语义校验（编译层 §3 F2）。
// 同阶段收集不中断：返回全部错误。certs 抽象证书文件检查（IO），测试可注入假实现。
func link(cs *protocol.ConfigSet, opts Options, certs certVerifier) (*linked, []CompileError) {
	// 默认值填充在 F2 完成（编译层 §3 F2）；须在 address:port 唯一性等
	// 依赖默认值的规则之前执行（如两个 Listener 都省略 address 时取 0.0.0.0）。
	protocol.ApplyDefaults(cs)

	lk := &linked{
		listeners: map[string]*protocol.Listener{},
		upstreams: map[string]*protocol.Upstream{},
		policies:  map[string]*protocol.Policy{},
	}

	var errs []CompileError
	errs = append(errs, resolveReferences(cs, lk)...)
	errs = append(errs, checkListenerAddressUnique(cs)...)
	errs = append(errs, checkRouteForm(cs, lk)...)
	errs = append(errs, checkL4RouteConstraints(cs, lk)...)
	errs = append(errs, checkHostnameConflicts(cs, lk)...)
	errs = append(errs, checkPolicyAttachmentLevels(cs, lk)...)
	errs = append(errs, checkTLSRules(cs)...)
	errs = append(errs, checkCertificates(cs, certs, opts)...)
	errs = append(errs, checkKubernetesService(cs, opts)...)
	return lk, errs
}

// isHTTPProtocol 报告协议是否为 HTTP 类（可挂 rules 形态 Route）。
func isHTTPProtocol(p protocol.ListenerProtocol) bool {
	return p == protocol.ProtocolHTTP || p == protocol.ProtocolHTTPS
}

// resolveReferences 建立全部 name 引用的指针索引；悬空引用报错（路径精确到字段）。
// 同 kind 重名已在 loader 检测（F1），此处直接按 name 建索引。
func resolveReferences(cs *protocol.ConfigSet, lk *linked) []CompileError {
	for _, l := range cs.Listeners {
		lk.listeners[l.Metadata.Name] = l
	}
	for _, u := range cs.Upstreams {
		lk.upstreams[u.Metadata.Name] = u
	}
	for _, p := range cs.Policies {
		lk.policies[p.Metadata.Name] = p
	}

	var errs []CompileError

	// Gateway / Listener 级策略引用。
	if cs.Gateway != nil {
		errs = append(errs, resolvePolicyRefs(cs.Gateway.Origin, protocol.KindGateway,
			cs.Gateway.Metadata.Name, "spec.policies", cs.Gateway.Spec.Policies, lk)...)
	}
	for _, l := range cs.Listeners {
		errs = append(errs, resolvePolicyRefs(l.Origin, protocol.KindListener,
			l.Metadata.Name, "spec.policies", l.Spec.Policies, lk)...)
	}

	for _, r := range cs.Routes {
		// Route.spec.listeners → Listener。
		for i, name := range r.Spec.Listeners {
			if _, ok := lk.listeners[name]; !ok {
				errs = append(errs, linkError(r.Origin, protocol.KindRoute, r.Metadata.Name,
					fmt.Sprintf("spec.listeners[%d]", i),
					"dangling reference: listener %q not found", name))
			}
		}
		// Route 级策略引用。
		errs = append(errs, resolvePolicyRefs(r.Origin, protocol.KindRoute,
			r.Metadata.Name, "spec.policies", r.Spec.Policies, lk)...)
		// L4 形态：forward.upstream → Upstream。
		if f := r.Spec.Forward; f != nil {
			if _, ok := lk.upstreams[f.Upstream]; !ok {
				errs = append(errs, linkError(r.Origin, protocol.KindRoute, r.Metadata.Name,
					"spec.forward.upstream",
					"dangling reference: upstream %q not found", f.Upstream))
			}
		}
		for i, rule := range r.Spec.Rules {
			rulePath := fmt.Sprintf("spec.rules[%d]", i)
			// backends[].upstream → Upstream。
			for j, b := range rule.Backends {
				if _, ok := lk.upstreams[b.Upstream]; !ok {
					errs = append(errs, linkError(r.Origin, protocol.KindRoute, r.Metadata.Name,
						fmt.Sprintf("%s.backends[%d].upstream", rulePath, j),
						"dangling reference: upstream %q not found", b.Upstream))
				}
			}
			// rule 级策略引用。
			errs = append(errs, resolvePolicyRefs(r.Origin, protocol.KindRoute,
				r.Metadata.Name, rulePath+".policies", rule.Policies, lk)...)
		}
	}
	return errs
}

// resolvePolicyRefs 校验一组 policies 列表中的字符串引用（Ref 形态）均可解析到 Policy。
// 内联匿名对象（Inline）无需解析。
func resolvePolicyRefs(origin protocol.Origin, kind protocol.Kind, name, path string,
	policies []protocol.PolicyAttachment, lk *linked,
) []CompileError {
	var errs []CompileError
	for i, p := range policies {
		if p.Ref == "" {
			continue
		}
		if _, ok := lk.policies[p.Ref]; !ok {
			errs = append(errs, linkError(origin, kind, name,
				fmt.Sprintf("%s[%d]", path, i),
				"dangling reference: policy %q not found", p.Ref))
		}
	}
	return errs
}

// checkListenerAddressUnique 校验 Listener address:port 组合在配置集内唯一（协议 §3.2 约束）。
// 须在 ApplyDefaults 之后运行（address 默认 0.0.0.0 参与比较）。
func checkListenerAddressUnique(cs *protocol.ConfigSet) []CompileError {
	var errs []CompileError
	seen := map[string]*protocol.Listener{}
	for _, l := range cs.Listeners {
		key := net.JoinHostPort(l.Spec.Address, fmt.Sprint(l.Spec.Port))
		if prev, ok := seen[key]; ok {
			errs = append(errs, linkError(l.Origin, protocol.KindListener, l.Metadata.Name,
				"spec.port",
				"address:port %q conflicts with listener %q (must be unique)", key, prev.Metadata.Name))
			continue
		}
		seen[key] = l
	}
	return errs
}

// checkRouteForm 校验 Route 形态与挂接 Listener 的协议匹配（协议 §3.3.5）：
//   - rules 与 forward 互斥且必选其一；
//   - HTTP 类 Listener（HTTP/HTTPS）只挂 rules 形态；L4 类（TCP/TLS/UDP）只挂 forward 形态；
//   - forward.sniHosts 仅在 protocol: TLS 的 Listener 上可用。
func checkRouteForm(cs *protocol.ConfigSet, lk *linked) []CompileError {
	var errs []CompileError
	for _, r := range cs.Routes {
		hasRules := len(r.Spec.Rules) > 0
		hasForward := r.Spec.Forward != nil
		switch {
		case hasRules && hasForward:
			errs = append(errs, linkError(r.Origin, protocol.KindRoute, r.Metadata.Name,
				"spec.forward", "rules and forward are mutually exclusive (HTTP rules form vs L4 forward form)"))
			continue
		case !hasRules && !hasForward:
			errs = append(errs, linkError(r.Origin, protocol.KindRoute, r.Metadata.Name,
				"spec", "route must have either rules (HTTP form) or forward (L4 form)"))
			continue
		}
		for i, name := range r.Spec.Listeners {
			l, ok := lk.listeners[name]
			if !ok {
				continue // 悬空引用已由 resolveReferences 报错
			}
			switch {
			case isHTTPProtocol(l.Spec.Protocol) && hasForward:
				errs = append(errs, linkError(r.Origin, protocol.KindRoute, r.Metadata.Name,
					fmt.Sprintf("spec.listeners[%d]", i),
					"route form mismatch: listener %q is %s (HTTP-class) but route uses forward (L4 form)",
					name, l.Spec.Protocol))
			case !isHTTPProtocol(l.Spec.Protocol) && hasRules:
				errs = append(errs, linkError(r.Origin, protocol.KindRoute, r.Metadata.Name,
					fmt.Sprintf("spec.listeners[%d]", i),
					"route form mismatch: listener %q is %s (L4-class) but route uses rules (HTTP form)",
					name, l.Spec.Protocol))
			}
			// sniHosts 仅 protocol: TLS 的 listener 可用（协议 §3.3.5）。
			if hasForward && len(r.Spec.Forward.SNIHosts) > 0 && l.Spec.Protocol != protocol.ProtocolTLS {
				errs = append(errs, linkError(r.Origin, protocol.KindRoute, r.Metadata.Name,
					"spec.forward.sniHosts",
					"sniHosts is only allowed on listeners with protocol TLS, but listener %q is %s",
					name, l.Spec.Protocol))
			}
		}
	}
	return errs
}

// checkL4RouteConstraints 校验能够无歧义地映射为 Envoy network/UDP filter 的
// L4 路由集合：TCP/UDP 恰好一个 forward；TLS 至少一个 forward，且每条 route
// 必须声明非空、大小写不敏感且不重复的 SNI。L4 Listener/Route 不接受 HTTP
// Policy；全局 Gateway Policy 只作用于 HTTP Listener，因此不在这里禁止。
func checkL4RouteConstraints(cs *protocol.ConfigSet, lk *linked) []CompileError {
	type attachedRoute struct {
		route         *protocol.Route
		listenerIndex int
	}
	attached := map[string][]attachedRoute{}
	var errs []CompileError

	for _, l := range cs.Listeners {
		if isHTTPProtocol(l.Spec.Protocol) {
			continue
		}
		if l.Spec.HTTP != nil {
			errs = append(errs, linkError(l.Origin, protocol.KindListener, l.Metadata.Name,
				"spec.http", "spec.http is not allowed on %s listener %q", l.Spec.Protocol, l.Metadata.Name))
		}
		for i := range l.Spec.Policies {
			errs = append(errs, linkError(l.Origin, protocol.KindListener, l.Metadata.Name,
				fmt.Sprintf("spec.policies[%d]", i),
				"HTTP policy is not allowed on %s listener %q", l.Spec.Protocol, l.Metadata.Name))
		}
	}

	for _, r := range cs.Routes {
		if r.Spec.Forward == nil || len(r.Spec.Rules) > 0 {
			continue // 形态错误由 checkRouteForm 负责，避免级联噪声
		}
		if len(r.Spec.Hostnames) > 0 {
			errs = append(errs, linkError(r.Origin, protocol.KindRoute, r.Metadata.Name,
				"spec.hostnames", "hostnames are not allowed on an L4 forward route; use forward.sniHosts for TLS"))
		}
		for i, listenerName := range r.Spec.Listeners {
			l, ok := lk.listeners[listenerName]
			if !ok || isHTTPProtocol(l.Spec.Protocol) {
				continue
			}
			attached[listenerName] = append(attached[listenerName], attachedRoute{route: r, listenerIndex: i})
		}
		if len(r.Spec.Policies) > 0 {
			for i := range r.Spec.Policies {
				errs = append(errs, linkError(r.Origin, protocol.KindRoute, r.Metadata.Name,
					fmt.Sprintf("spec.policies[%d]", i),
					"HTTP policy is not allowed on an L4 forward route"))
			}
		}
	}

	for _, l := range sortedListeners(cs.Listeners) {
		if isHTTPProtocol(l.Spec.Protocol) {
			continue
		}
		routes := attached[l.Metadata.Name]
		if l.Spec.Protocol == protocol.ProtocolTCP || l.Spec.Protocol == protocol.ProtocolUDP {
			switch len(routes) {
			case 0:
				errs = append(errs, linkError(l.Origin, protocol.KindListener, l.Metadata.Name,
					"spec.protocol", "protocol %s requires exactly one attached forward route, got 0", l.Spec.Protocol))
			case 1:
				// valid
			default:
				for _, a := range routes[1:] {
					errs = append(errs, linkError(a.route.Origin, protocol.KindRoute, a.route.Metadata.Name,
						fmt.Sprintf("spec.listeners[%d]", a.listenerIndex),
						"listener %q with protocol %s accepts exactly one forward route (already attached by route %q)",
						l.Metadata.Name, l.Spec.Protocol, routes[0].route.Metadata.Name))
				}
			}
			continue
		}

		if len(routes) == 0 {
			errs = append(errs, linkError(l.Origin, protocol.KindListener, l.Metadata.Name,
				"spec.protocol", "protocol TLS requires at least one attached forward route with sniHosts"))
			continue
		}
		claimed := map[string]string{}
		for _, a := range routes {
			hosts := a.route.Spec.Forward.SNIHosts
			if len(hosts) == 0 {
				errs = append(errs, linkError(a.route.Origin, protocol.KindRoute, a.route.Metadata.Name,
					"spec.forward.sniHosts", "at least one sniHost is required on TLS listener %q", l.Metadata.Name))
				continue
			}
			for i, host := range hosts {
				norm := strings.ToLower(strings.TrimSpace(host))
				path := fmt.Sprintf("spec.forward.sniHosts[%d]", i)
				if norm == "" {
					errs = append(errs, linkError(a.route.Origin, protocol.KindRoute, a.route.Metadata.Name,
						path, "sniHost must not be empty"))
					continue
				}
				if prev, ok := claimed[norm]; ok {
					errs = append(errs, linkError(a.route.Origin, protocol.KindRoute, a.route.Metadata.Name,
						path, "sniHost %q on listener %q conflicts with route %q", host, l.Metadata.Name, prev))
					continue
				}
				claimed[norm] = a.route.Metadata.Name
			}
		}
	}
	return errs
}

// checkHostnameConflicts 校验同一 Listener 下各 Route 的 hostname 同精度不重复
// （协议 §3.3 要点 1：精确 > 通配 > 兜底，同精度冲突 = 编译错误）。
// 仅对 HTTP 形态的 Route 生效（forward 形态无 hostnames）；hostname 比较大小写不敏感。
func checkHostnameConflicts(cs *protocol.ConfigSet, lk *linked) []CompileError {
	var errs []CompileError
	// listener → 归一化 hostname → 先声明的 Route。
	type claim struct {
		route string
	}
	claimed := map[string]map[string]claim{}
	for _, r := range cs.Routes {
		if len(r.Spec.Rules) == 0 {
			continue // 非 HTTP 形态（含形态错误，已由 checkRouteForm 报错）
		}
		hostnames := r.Spec.Hostnames
		if len(hostnames) == 0 {
			hostnames = []string{"*"} // 省略 = 兜底 "*"
		}
		for _, lisName := range r.Spec.Listeners {
			if _, ok := lk.listeners[lisName]; !ok {
				continue // 悬空引用已报错
			}
			m := claimed[lisName]
			if m == nil {
				m = map[string]claim{}
				claimed[lisName] = m
			}
			for i, h := range hostnames {
				norm := strings.ToLower(h)
				if prev, ok := m[norm]; ok && prev.route != r.Metadata.Name {
					path := "spec.hostnames"
					if len(r.Spec.Hostnames) > 0 {
						path = fmt.Sprintf("spec.hostnames[%d]", i)
					}
					errs = append(errs, linkError(r.Origin, protocol.KindRoute, r.Metadata.Name, path,
						"hostname %q on listener %q conflicts with route %q (same match precision: exact > wildcard > fallback)",
						h, lisName, prev.route))
					continue
				}
				m[norm] = claim{route: r.Metadata.Name}
			}
		}
	}
	return errs
}

// attachLevel 是策略挂接层级（协议 §3.5 规则 1）。
type attachLevel int

// 策略挂接的四级。
const (
	levelGateway attachLevel = iota
	levelListener
	levelRoute
	levelRule
)

// checkPolicyAttachmentLevels 校验策略挂接层级合法性（编译层 §3 F2）。
// v0 唯一的层级限制：ipAccess 不可挂 rule 级（IP 黑白名单作用于连接/入口粒度，
// 按 rule 级 per-route 配置无意义）。
func checkPolicyAttachmentLevels(cs *protocol.ConfigSet, lk *linked) []CompileError {
	var errs []CompileError
	check := func(origin protocol.Origin, kind protocol.Kind, name, path string,
		policies []protocol.PolicyAttachment, level attachLevel,
	) {
		for i, p := range policies {
			typ := policyTypeOf(p, lk)
			if typ == "" {
				continue // 悬空引用已报错
			}
			if typ == "ipAccess" && level == levelRule {
				errs = append(errs, linkError(origin, kind, name,
					fmt.Sprintf("%s[%d]", path, i),
					"ipAccess policy cannot be attached at rule level (allowed: gateway | listener | route)"))
			}
		}
	}
	if cs.Gateway != nil {
		check(cs.Gateway.Origin, protocol.KindGateway, cs.Gateway.Metadata.Name,
			"spec.policies", cs.Gateway.Spec.Policies, levelGateway)
	}
	for _, l := range cs.Listeners {
		check(l.Origin, protocol.KindListener, l.Metadata.Name,
			"spec.policies", l.Spec.Policies, levelListener)
	}
	for _, r := range cs.Routes {
		check(r.Origin, protocol.KindRoute, r.Metadata.Name,
			"spec.policies", r.Spec.Policies, levelRoute)
		for i, rule := range r.Spec.Rules {
			check(r.Origin, protocol.KindRoute, r.Metadata.Name,
				fmt.Sprintf("spec.rules[%d].policies", i), rule.Policies, levelRule)
		}
	}
	return errs
}

// policyTypeOf 返回挂接的策略类型键；引用悬空时返回 ""。
func policyTypeOf(p protocol.PolicyAttachment, lk *linked) string {
	spec := p.Inline
	if spec == nil {
		pol, ok := lk.policies[p.Ref]
		if !ok {
			return ""
		}
		spec = &pol.Spec
	}
	return policyTypeKey(spec)
}

// policyTypeKey 返回 PolicySpec 的类型键（单类型键已由 loader 校验）。
func policyTypeKey(s *protocol.PolicySpec) string {
	switch {
	case s.HeaderModifier != nil:
		return "headerModifier"
	case s.CORS != nil:
		return "cors"
	case s.RateLimit != nil:
		return "rateLimit"
	case s.JWT != nil:
		return "jwt"
	case s.ExtAuth != nil:
		return "extAuth"
	case s.IPAccess != nil:
		return "ipAccess"
	case s.BasicAuth != nil:
		return "basicAuth"
	}
	return ""
}

// checkTLSRules 校验 TLS 终止配置（协议 §3.2）：HTTPS 必须有 spec.tls；
// HTTP/TCP/TLS passthrough/UDP 不得有，避免用户误以为 passthrough 会终止 TLS。
func checkTLSRules(cs *protocol.ConfigSet) []CompileError {
	var errs []CompileError
	for _, l := range cs.Listeners {
		switch l.Spec.Protocol {
		case protocol.ProtocolHTTPS:
			if l.Spec.TLS == nil {
				errs = append(errs, linkError(l.Origin, protocol.KindListener, l.Metadata.Name,
					"spec.tls", "spec.tls is required for protocol %s", l.Spec.Protocol))
			}
		case protocol.ProtocolHTTP, protocol.ProtocolTCP, protocol.ProtocolTLS, protocol.ProtocolUDP:
			if l.Spec.TLS != nil {
				errs = append(errs, linkError(l.Origin, protocol.KindListener, l.Metadata.Name,
					"spec.tls", "spec.tls is not allowed for protocol %s", l.Spec.Protocol))
			}
		}
	}
	return errs
}

// checkCertificates 校验证书条目（协议 §3.2，编译层 §3 F2）：
//   - ref: 形态解析到托管证书目录并按普通文件对继续校验；
//   - certFile/keyFile 文件存在、可解析且公钥配对（openssl 语义）；
//   - clientCA 文件存在且可解析。
//
// static 模式下发前还会再查一次，此处早失败。
func checkCertificates(cs *protocol.ConfigSet, certs certVerifier, opts Options) []CompileError {
	var errs []CompileError
	for _, l := range cs.Listeners {
		t := l.Spec.TLS
		if t == nil {
			continue
		}
		for i, c := range t.Certificates {
			base := fmt.Sprintf("spec.tls.certificates[%d]", i)
			if c.Ref != "" {
				if opts.ManagedCertificateDir == "" {
					errs = append(errs, linkError(l.Origin, protocol.KindListener, l.Metadata.Name,
						base+".ref", "managed certificate store is unavailable for ref %q", c.Ref))
					continue
				}
				if err := protocol.ValidateName(c.Ref); err != nil {
					errs = append(errs, linkError(l.Origin, protocol.KindListener, l.Metadata.Name,
						base+".ref", "invalid managed certificate ref %q", c.Ref))
					continue
				}
				c.CertFile = filepath.Join(opts.ManagedCertificateDir, c.Ref, "tls.crt")
				c.KeyFile = filepath.Join(opts.ManagedCertificateDir, c.Ref, "tls.key")
				t.Certificates[i] = c
			}
			if err := certs.verifyKeyPair(c.CertFile, c.KeyFile); err != nil {
				errs = append(errs, linkError(l.Origin, protocol.KindListener, l.Metadata.Name,
					base+".certFile",
					"invalid certificate pair (certFile %q, keyFile %q): %v", c.CertFile, c.KeyFile, err))
			}
		}
		if t.ClientCA != "" {
			if err := certs.verifyCAFile(t.ClientCA); err != nil {
				errs = append(errs, linkError(l.Origin, protocol.KindListener, l.Metadata.Name,
					"spec.tls.clientCA", "invalid clientCA file %q: %v", t.ClientCA, err))
			}
		}
	}
	return errs
}

// checkKubernetesService 校验 kubernetesService 端点来源在当前环境可用：
// EndpointSource 未注入（M0 恒 nil）时报错（编译层 §2，technical_design §3）。
func checkKubernetesService(cs *protocol.ConfigSet, opts Options) []CompileError {
	if opts.EndpointSource != nil {
		return nil
	}
	var errs []CompileError
	for _, u := range cs.Upstreams {
		if u.Spec.KubernetesService != nil {
			errs = append(errs, linkError(u.Origin, protocol.KindUpstream, u.Metadata.Name,
				"spec.kubernetesService",
				"kubernetesService endpoint source is unavailable: k8s service discovery is not enabled in this environment (EndpointSource not injected)"))
		}
	}
	return errs
}
