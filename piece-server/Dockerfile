ARG CURIO_IMAGE=curio:latest
#############################################################################
FROM curio:latest as curio-builder
FROM lotus:latest as lotus-builder
COPY --from=curio-builder /opt/curio /opt/curio
COPY --from=lotus-builder /usr/local/bin /usr/local/bin

EXPOSE 12320

VOLUME /var/tmp/filecoin-proof-parameters
VOLUME /var/lib/genesis
VOLUME /var/lib/builtin-actors

WORKDIR /opt/curio
RUN mkdir -p /app

COPY start_scripts/init.sh /app
COPY sample/* /app/sample/

USER root

ENTRYPOINT ["./init.sh"]