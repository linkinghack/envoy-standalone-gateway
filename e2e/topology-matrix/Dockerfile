FROM envoyproxy/envoy:v1.39.0
COPY esgw /usr/local/bin/esgw
COPY management-loop.sh /usr/local/bin/esgw-management-loop
COPY managed-xds.yaml managed-static.yaml external-xds.yaml external-static.yaml /fixtures/
COPY gateway-old.yaml gateway-new.yaml gateway-invalid.yaml external-xds-bootstrap.yaml /fixtures/
RUN chmod 0755 /usr/local/bin/esgw /usr/local/bin/esgw-management-loop \
    && mkdir -p /var/lib/esgw/config.d /var/lib/esgw/envoy /var/lib/esgw/run /run
ENTRYPOINT []
