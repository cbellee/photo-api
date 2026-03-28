FROM golang:1.26-alpine AS builder
WORKDIR /build
COPY . .
RUN go mod tidy && CGO_ENABLED=0 go build -o blobemu .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /build/blobemu .

EXPOSE 10000
VOLUME ["/data"]
CMD ["./blobemu"]
