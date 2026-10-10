FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY core ./core
COPY server ./server
COPY cmd ./cmd
RUN CGO_ENABLED=0 go build -trimpath -o /klefki-server ./cmd/klefki-server

FROM alpine:3.23
RUN addgroup -g 10001 klefki && adduser -D -u 10001 -G klefki klefki \
    && mkdir /data && chown klefki:klefki /data
COPY --from=build /klefki-server /usr/local/bin/klefki-server
USER 10001:10001
EXPOSE 8443
ENTRYPOINT ["klefki-server"]
CMD ["-listen", "0.0.0.0:8443", "-data", "/data", "-tls-cert", "/tls/server.crt", "-tls-key", "/tls/server.key"]
