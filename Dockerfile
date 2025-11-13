FROM golang:alpine as builder

RUN apk add upx

WORKDIR /app
COPY . entrypoint.sh /app/

RUN go build -ldflags="-s -w" -trimpath ./... && \
    upx --best --lzma ./apk-cache

FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/apk-cache /app/entrypoint.sh /app/

CMD sh /app/entrypoint.sh
