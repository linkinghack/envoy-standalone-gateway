# Envoy Standalone Gateway

**A general-purpose standalone API gateway & reverse proxy based on Envoy, with a professional admin UI.**

Envoy is arguably the most capable proxy data plane in existence — yet outside of Kubernetes / service-mesh ecosystems, it is rarely used as a general reverse proxy, because hand-writing Envoy configuration is hard, and the control planes built around it all assume a Kubernetes-shaped world.

**Envoy Standalone Gateway makes Envoy a first-class Nginx replacement**: a self-contained gateway you can run on a bare Linux box with systemd, in a single Docker container, or (optionally, not necessarily) in Kubernetes.

## Why

- **Nginx-style simplicity, Envoy-grade power.** Hot config reload without dropping connections, first-class observability, HTTP/2 & HTTP/3 & gRPC native support, rich L4/L7 filters — capabilities Nginx users pay for or patch in, Envoy has built in.
- **No Kubernetes required.** Every existing serious Envoy control plane (Istio, Contour, Envoy Gateway, ...) assumes k8s. This project treats k8s as an *optional enhancement*, never a dependency.
- **A config model humans can actually understand.** Raw Envoy static config is expert-level material; Nginx config is arcane in its own way; Istio's Gateway/VirtualService split confuses newcomers. We define a **platform-neutral, easy-to-understand, extensible gateway configuration protocol** that compiles down to native Envoy configuration — while still letting power users write raw Envoy static config directly.
- **A professional admin console, open source.** Live view of effective Envoy state (clusters, listeners, endpoints, routes), traffic statistics, and full configuration management through a web UI — the kind of console usually locked behind commercial licenses. We open-source it.

## What it is

A lightweight management plane (single binary + web UI) that drives one or more Envoy instances:

```
                        ┌─────────────────────────────┐
   Admin UI (Web) ───►  │  Management Plane            │
   REST/CLI       ───►  │  · gateway config abstraction│
                        │  · validation & compilation  │
                        └──────┬───────────────┬──────┘
                               │               │
                     static YAML render     xDS server
                               │               │
                               ▼               ▼
                        ┌─────────────────────────────┐
                        │        Envoy (data plane)    │
                        └─────────────────────────────┘
```

- **Two delivery modes** for driving Envoy: generated **static YAML** (simple, file-based, systemd-friendly) or a live **xDS control plane** (dynamic, zero-restart updates).
- **Two authoring modes** for users: the simplified **gateway configuration protocol**, or **raw Envoy static config** for full control. Both compile to the same Envoy configuration objects.
- **Environment-aware**: when running inside Kubernetes it can auto-discover Services as upstream candidates; on plain hosts it just works with IPs, hostnames and DNS.

## Status

🚧 **Early design stage.** See [`design_docs/`](design_docs/) for requirements analysis and system design documents.

## Deployment targets

| Target | Support |
|---|---|
| Linux native (systemd) | ✅ first-class |
| Docker / OCI container | ✅ first-class |
| Kubernetes | ✅ supported, with extra conveniences — but never required |

## License

[AGPL-3.0](LICENSE). You may use, modify and redistribute this software freely, including commercially — but any modified version you distribute **or operate as a network service** must be open-sourced under the same license. Closed-source repackaging and resale is not permitted.
