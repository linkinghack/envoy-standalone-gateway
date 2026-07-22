# Downstream mTLS and circuit-breaker real-traffic test

`run.sh` consumes `testdata/mtls-circuit/want-static.yaml` on every supported Envoy version. It proves that a missing or untrusted client certificate is rejected, a client signed by `client-ca.crt` is accepted, and `maxConnections=1` plus `maxPendingRequests=1` produces real HTTP 503 overflow responses under concurrent load.

Run with `make e2e-mtls-circuit`. `DOCKER` and `E2E_MTLS_PORT` may override the Docker CLI and host port.
