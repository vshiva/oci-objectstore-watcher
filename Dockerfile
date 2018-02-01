FROM alpine:3.6

# Update CA Certs and OpenSSL
RUN apk add --update ca-certificates openssl

COPY oci-objectstore-watcher /usr/local/bin/oci-objectstore-watcher

WORKDIR /

ENTRYPOINT ["oci-objectstore-watcher"]

CMD ["version"]
