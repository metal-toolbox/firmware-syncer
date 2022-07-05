FROM alpine:latest

ENTRYPOINT ["/usr/sbin/firmware-syncer"]

COPY firmware-syncer /usr/sbin/firmware-syncer
RUN chmod +x /usr/sbin/firmware-syncer

