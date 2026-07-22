FROM envoyproxy/envoy:v1.39.0

USER root
COPY esgw /usr/local/bin/esgw
COPY esgw.yaml /etc/esgw/esgw.yaml
COPY gateway.yaml /var/lib/esgw/config.d/gateway.yaml
COPY entrypoint.sh /usr/local/bin/esgw-e2e-entrypoint
RUN chmod 0755 /usr/local/bin/esgw /usr/local/bin/esgw-e2e-entrypoint \
    && mkdir -p /var/lib/esgw/envoy /var/lib/esgw/run \
    && chmod 0700 /var/lib/esgw /var/lib/esgw/config.d /var/lib/esgw/envoy /var/lib/esgw/run

ENTRYPOINT ["/usr/local/bin/esgw-e2e-entrypoint"]
