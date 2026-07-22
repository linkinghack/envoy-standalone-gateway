# External authorization real-traffic smoke

`run.sh` starts a real HTTP and gRPC authorization service, an application backend, and Envoy from the extAuth golden artifact. It covers allow, deny, route disable, HTTP fail-open, and gRPC fail-closed behavior.
