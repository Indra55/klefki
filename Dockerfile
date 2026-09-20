FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY core ./core
COPY server ./server
COPY cmd ./cmd
RUN CGO_ENABLED=0 go build -trimpath -o /vault-server ./cmd/vault-server

FROM alpine:3.23
RUN addgroup -g 10001 vault && adduser -D -u 10001 -G vault vault \
    && mkdir /data && chown vault:vault /data
COPY --from=build /vault-server /usr/local/bin/vault-server
USER 10001:10001
EXPOSE 8443
ENTRYPOINT ["vault-server"]
CMD ["-listen", "0.0.0.0:8443", "-data", "/data", "-tls-cert", "/tls/server.crt", "-tls-key", "/tls/server.key"]
