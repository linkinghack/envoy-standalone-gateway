# P1 combined ADS real-traffic matrix

This scenario runs one combined P1 configuration through both static and xDS compilation, validates the static artifact, then connects Envoy to the real ADS server. On every supported Envoy version it exercises TCP, TLS passthrough/SNI, UDP, HTTP/gRPC external authorization, IP access and dynamic-key quotas, downstream mTLS, and circuit-breaker overflow.

The management plane, Envoy, and backends share a network namespace so deterministic loopback upstreams remain unchanged. Source-IP policy clients stay on the surrounding fixed Docker subnet with distinct addresses. Runtime files are copied into containers; no host bind mount is required.

The test deliberately copies `testdata/certs` to both the management-plane and Envoy filesystems. SDS carries stable Secret identities and update semantics, while the current Secret payload still uses filename DataSources. Cross-host secret-material transport is a P2 concern and is not implied by this test.

Run with `make e2e-p1-xds`; `DOCKER` may override the Docker CLI.

Host ports default to TCP `14306`, TLS passthrough `19443`, UDP `16353`, HTTP/gRPC extAuth `18181`/`18182`, mTLS `19444`, and Envoy admin `19911`. Each can be overridden with the corresponding `E2E_P1_*_PORT` environment variable.
