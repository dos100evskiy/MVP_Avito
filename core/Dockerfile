# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS builder
WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/core ./cmd/core

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/core /app/core
EXPOSE 8080
ENTRYPOINT ["/app/core"]
