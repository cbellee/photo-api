FROM golang:1.26 AS builder

ARG SERVICE_NAME=""
ARG SERVICE_PORT=""

RUN mkdir /build
WORKDIR /build
COPY ./api .

# install dependencies
WORKDIR /build/cmd
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o server ./${SERVICE_NAME}/

FROM scratch

ARG SERVICE_NAME=""
ARG SERVICE_PORT=""

WORKDIR /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /build/cmd/server .

# run server
EXPOSE $SERVICE_PORT
CMD ["./server"]