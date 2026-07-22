# IP access and local rate-limit real-traffic test

`run.sh` consumes `testdata/ipaccess/want-static.yaml` and runs the same artifact on every supported Envoy version. It verifies:

- IP allow-list, deny precedence, and an address outside the allow-list;
- untrusted `X-Forwarded-For` cannot change the physical peer identity;
- two client IP values receive independent token buckets;
- two `x-tenant` values receive independent token buckets;
- a missing keyed header is limited by the route's default fallback bucket.

The test uses fixed, isolated Docker subnets because source addresses are part of the assertions. Run it with `make e2e-policy`; `DOCKER` may override the Docker CLI.
