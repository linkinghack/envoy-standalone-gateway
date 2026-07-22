# L4 real-traffic smoke

`run.sh` consumes `testdata/l4/want-static.yaml` and starts real TCP, TLS, and UDP backends plus Envoy. It verifies raw TCP forwarding, two TLS passthrough SNI routes, unknown-SNI rejection, and UDP request/reply forwarding.

Run with:

```bash
make e2e-l4
```

`ENVOY_IMAGE` and `DOCKER` may override the default Envoy image and Docker CLI.
