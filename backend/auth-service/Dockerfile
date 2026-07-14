# build application
FROM golang:1.26.4-alpine AS builder
RUN apk add --no-cache git ca-certificates
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o service main.go
# Run application
FROM alpine:latest
RUN apk add --no-cache tzdata git
WORKDIR /app
COPY --from=builder /build/service .
EXPOSE 11002
ENTRYPOINT [ "./service" ]
