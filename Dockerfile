# ## Kullanım örneği
#
# ```bash
# docker build -t freebuff-proxy:local .
# docker run --rm -p 1455:1455 -e FREEBUFF_PROXY_ADDR=0.0.0.0:1455 freebuff-proxy:local serve
# ```
FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/freebuff-proxy ./cmd/freebuff-proxy

FROM alpine:3.20 AS runtime

RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 freebuff \
    && adduser -S -D -h /home/freebuff -u 10001 -G freebuff freebuff \
    && mkdir -p /home/freebuff/.config/manicode \
    && chown -R freebuff:freebuff /home/freebuff

COPY --from=build /out/freebuff-proxy /usr/local/bin/freebuff-proxy

USER freebuff
EXPOSE 1455

ENTRYPOINT ["freebuff-proxy"]
CMD ["serve"]
