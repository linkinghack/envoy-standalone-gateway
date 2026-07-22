# ESGW gateway configuration protocol `esgw/v1alpha1`

Status: Alpha
Schema ID: `https://linkinghack.com/esgw/schemas/v1alpha1.json`

## 1. Document model

A configuration is a directory of `.yaml` or `.yml` files. Files are read in lexical order and may contain multiple YAML documents separated by `---`. Every non-empty document has this envelope:

```yaml
apiVersion: esgw/v1alpha1
kind: Listener
metadata:
  name: web
  labels: {team: edge}
spec: {}
```

`metadata.name` must match `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`. Names are unique within each kind. Unknown fields, YAML aliases, unsupported versions, and unsupported kinds are errors.

The six kinds are:

| Kind | Purpose |
|---|---|
| `Gateway` | singleton global HTTP/access-log defaults; the only valid name is `default` |
| `Listener` | downstream socket and HTTP/HTTPS/TCP/TLS/UDP protocol |
| `Route` | HTTP rules or one L4 forwarding rule attached to listeners |
| `Upstream` | endpoints, DNS, Kubernetes service reference, TLS, health, and connection protection |
| `Policy` | reusable HTTP policy |
| `EnvoyResources` | validated Envoy v3 escape-hatch resources |

The JSON Schema validates document structure. `esgw conformance` additionally resolves references, applies defaults, builds both protocol and Envoy semantics, and validates the xDS resource graph.

## 2. Listener and routing semantics

Listener protocol values are `HTTP`, `HTTPS`, `TCP`, `TLS`, and `UDP`.

- `HTTP` and `HTTPS` consume `Route.spec.rules` in declaration order; the first matching rule wins.
- `HTTPS` terminates TLS and requires `spec.tls.certificates`. `clientCA` enables mandatory downstream client-certificate validation.
- `TCP` has exactly one `Route.spec.forward` without SNI hosts.
- `TLS` is passthrough, not termination. Every forwarding route has one or more unique `sniHosts`; `Listener.spec.tls` is forbidden.
- `UDP` has exactly one `Route.spec.forward` and does not accept HTTP fields or policies.

An HTTP rule action is exactly one of `backends`, `redirect`, or `directResponse`. L4 routes use `forward` instead of `rules`. References to listeners, upstreams, policies, and patch targets must resolve.

## 3. Policy composition

Policy attachments may be a reusable Policy name or an inline Policy spec. At each scope, a Policy spec has exactly one type key. Effective policy precedence is `rule > Route > Listener > Gateway`; a nearer policy of the same type replaces the complete outer policy.

HTTP filter order is fixed:

`ipAccess → cors → jwt → extAuth → rateLimit → router`

Supported policy types in this release are `headerModifier`, `cors`, `rateLimit`, `jwt`, `extAuth`, and `ipAccess`. `basicAuth` remains reserved and is rejected by compilation.

`rateLimit` is a process-local token bucket. Its key is `clientIP` (default) or `header:<valid HTTP field name>`. Distinct runtime values receive distinct buckets. `maxKeys` bounds the dynamic bucket LRU and defaults to 10000 (range 1..100000). A missing keyed header consumes the route's fallback bucket.

IP access uses `allow AND NOT deny`; deny always wins. With no allow entries, the default is allow. The standalone edge profile trusts the physical downstream peer (`use_remote_address=true`, zero trusted XFF hops), so a client-provided XFF value cannot spoof the identity. Deployments behind a trusted proxy require a future explicit trust-chain setting and must not silently trust arbitrary XFF.

## 4. TLS and connection protection

Downstream certificate and CA files are verified while compiling. Static output uses file data sources. xDS output distributes server certificates as `crt/<listener>/<index>` SDS Secrets and a client CA as `ca/<listener>` validation-context Secret. In P1 those Secrets retain filename DataSources, so every data-plane node must materialize the referenced paths; transporting inline or independently stored secret material across hosts is outside this protocol level.

`Upstream.spec.connection.maxConnections` and `maxPendingRequests` range from 1 through 2147483647 and map to Envoy circuit breakers at DEFAULT priority. Zero or negative values are invalid.

## 5. Defaults

Material defaults include:

- Listener address `0.0.0.0`;
- route timeout `15s`;
- Gateway HTTP idle timeout `60s`, maximum request headers `60 KiB`, server header `esgw`;
- upstream connect timeout `5s`, endpoint weight `1`, round-robin balancing;
- rate-limit burst equal to requests, key `clientIP`, maxKeys `10000`;
- TLS minimum version `1.2`.

Defaults are applied before reference linking and Envoy resource construction. Generated artifacts are deterministic for identical effective input.

## 6. Conformance diagnostics

`esgw conformance -f <directory>` writes one JSON report and exits 0 when valid, 1 when invalid, or 2 for command usage errors. Diagnostics are sorted deterministically and contain a stable stage-level code, stage, severity, source, and message.

Stable error codes are:

- `ESGW_SCHEMA_INVALID`
- `ESGW_LINK_INVALID`
- `ESGW_BUILD_INVALID`
- `ESGW_PATCH_INVALID`
- `ESGW_VALIDATE_INVALID`

Warnings use the same stage with `_WARNING`. Schema sources contain `file` and zero-based `docIndex`; later stages contain `file`, `kind`, `name`, and the protocol field `path` where available. Message wording adds detail but integrations should branch on `code`.

## 7. Versioning and compatibility

`v1alpha1` is the only accepted API version in this bundle. While alpha:

- new optional fields and new diagnostic detail may be added;
- defaults and existing field meaning must not change silently;
- field removal, rename, type narrowing, or semantic incompatibility requires a new API version and migration path;
- unknown fields remain errors in every version.

The generated schema is a release artifact, not a second source of truth. Protocol Go types generate it; the clean-diff gate fails when the committed file is stale. Promotion to beta or stable requires the S12 compatibility audit.
