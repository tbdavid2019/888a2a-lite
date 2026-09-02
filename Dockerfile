FROM golang:1.25.6 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/888a2a-lite ./cmd/888a2a-lite

FROM alpine:3.22

RUN addgroup -S lite && adduser -S -G lite lite && mkdir -p /data && chown -R lite:lite /data
COPY --from=builder /out/888a2a-lite /usr/local/bin/888a2a-lite

USER lite
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 CMD wget -q -O - http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/888a2a-lite"]
CMD ["server"]
