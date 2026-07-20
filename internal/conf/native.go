package conf

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	bootstrapv3 "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/linkinghack/envoy-standalone-gateway/internal/compile"
	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	yamlv3 "gopkg.in/yaml.v3"
)

// LoadNative parses native.yaml as an Envoy bootstrap and projects its static
// resources into the common IR shape. The parser is strict: unknown protobuf
// fields and malformed YAML are rejected before publishing.
func LoadNative(path string) (*ir.IR, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read native.yaml: %w", err)
	}
	return ParseNative(b)
}

// ParseNative parses a native Envoy bootstrap from YAML bytes.
func ParseNative(content []byte) (*ir.IR, error) {
	jsonDoc, err := yamlToJSON(content)
	if err != nil {
		return nil, fmt.Errorf("native YAML: %w", err)
	}
	var bootstrap bootstrapv3.Bootstrap
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(jsonDoc, &bootstrap); err != nil {
		return nil, fmt.Errorf("native bootstrap strict decode: %w", err)
	}
	out := &ir.IR{
		Listeners: map[string]*listenerv3.Listener{},
		Clusters:  map[string]*clusterv3.Cluster{},
		Routes:    map[string]*routev3.RouteConfiguration{},
		Endpoints: map[string]*endpointv3.ClusterLoadAssignment{},
		Secrets:   map[string]*tlsv3.Secret{},
		Bootstrap: proto.Clone(&bootstrap).(*bootstrapv3.Bootstrap),
		SourceMap: map[ir.ResourceKey]ir.SourceRef{},
	}
	if sr := bootstrap.GetStaticResources(); sr != nil {
		for _, resource := range sr.GetListeners() {
			if resource.GetName() == "" {
				return nil, errors.New("native listener has empty name")
			}
			out.Listeners[resource.GetName()] = proto.Clone(resource).(*listenerv3.Listener)
		}
		for _, resource := range sr.GetClusters() {
			if resource.GetName() == "" {
				return nil, errors.New("native cluster has empty name")
			}
			out.Clusters[resource.GetName()] = proto.Clone(resource).(*clusterv3.Cluster)
		}
		for _, resource := range sr.GetSecrets() {
			if resource.GetName() == "" {
				return nil, errors.New("native secret has empty name")
			}
			out.Secrets[resource.GetName()] = proto.Clone(resource).(*tlsv3.Secret)
		}
	}
	if err := validateNative(&bootstrap); err != nil {
		return nil, err
	}
	if errs := compile.ValidateIR(out); len(errs) > 0 {
		return nil, fmt.Errorf("native IR validation failed: %s", errs[0].Error())
	}
	version, err := out.ComputeVersion()
	if err != nil {
		return nil, fmt.Errorf("compute native IR version: %w", err)
	}
	out.Version = version
	return out, nil
}

func validateNative(b *bootstrapv3.Bootstrap) error {
	if b == nil {
		return errors.New("native bootstrap is empty")
	}
	if b.GetStaticResources() == nil {
		return errors.New("native bootstrap must define static_resources (dynamic xDS bootstrap is not a native static draft)")
	}
	return nil
}

func yamlToJSON(content []byte) ([]byte, error) {
	dec := yamlv3.NewDecoder(bytes.NewReader(content))
	var node yamlv3.Node
	if err := dec.Decode(&node); err != nil {
		return nil, err
	}
	if node.Kind == 0 {
		return []byte("null"), nil
	}
	value, err := yamlNodeValue(&node)
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func yamlNodeValue(n *yamlv3.Node) (any, error) {
	if n.Kind == yamlv3.DocumentNode {
		if len(n.Content) == 0 {
			return nil, nil
		}
		return yamlNodeValue(n.Content[0])
	}
	switch n.Kind {
	case yamlv3.MappingNode:
		m := make(map[string]any, len(n.Content)/2)
		for i := 0; i < len(n.Content); i += 2 {
			var key string
			if err := n.Content[i].Decode(&key); err != nil {
				return nil, fmt.Errorf("mapping key: %w", err)
			}
			v, err := yamlNodeValue(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			m[key] = v
		}
		return m, nil
	case yamlv3.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, child := range n.Content {
			v, err := yamlNodeValue(child)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yamlv3.ScalarNode:
		var value any
		if err := n.Decode(&value); err != nil {
			return nil, err
		}
		return value, nil
	case yamlv3.AliasNode:
		return nil, errors.New("YAML aliases are not supported")
	default:
		return nil, fmt.Errorf("unsupported YAML node kind %d", n.Kind)
	}
}
