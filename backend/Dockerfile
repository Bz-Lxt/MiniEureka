FROM golang:1.26.5-alpine3.24 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/minieureka ./cmd/minieureka

FROM alpine:3.22.1
RUN addgroup -S -g 10001 minieureka && adduser -S -D -H -u 10001 -G minieureka minieureka
COPY --from=build --chown=minieureka:minieureka /out/minieureka /usr/local/bin/minieureka
USER minieureka
EXPOSE 8080/tcp 7946/udp
ENTRYPOINT ["/usr/local/bin/minieureka"]
