FROM golang:1.25.4-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/agent-telegram .

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 telegram \
    && adduser -S -D -H -u 10001 -G telegram -h /data telegram \
    && mkdir -p /data \
    && chown 10001:10001 /data \
    && chmod 0700 /data

COPY --from=build /out/agent-telegram /usr/local/bin/agent-telegram

ENV HOME=/data \
    TZ=Asia/Shanghai \
    AGENT_TELEGRAM_SESSION_PROVIDER=file \
    AGENT_TELEGRAM_PROFILE=default

USER 10001:10001
EXPOSE 8080
VOLUME ["/data"]

ENTRYPOINT ["agent-telegram"]
CMD ["gateway", "--listen", "0.0.0.0", "--port", "8080"]
