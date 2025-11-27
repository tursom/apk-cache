FROM golang:alpine as builder

RUN apk add upx gzip

WORKDIR /app
COPY . entrypoint.sh /app/

RUN sh build.sh && \
    upx --best --lzma ./apk-cache

FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/apk-cache /app/entrypoint.sh /app/

CMD ["/app/entrypoint.sh"]
