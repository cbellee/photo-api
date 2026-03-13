FROM golang:1.26 AS builder

ARG SERVICE_NAME=""
ARG SERVICE_PORT=""

RUN mkdir /build
WORKDIR /build
COPY ./api .

# install dependencies
WORKDIR /build/cmd
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o server ./${SERVICE_NAME}/

RUN mkdir -m 1777 /tmp
RUN chmod a+rwx /tmp && chmod +t /tmp # Ensure sticky bit and universal write permissions

FROM scratch

ARG SERVICE_NAME=""
ARG SERVICE_PORT=""

WORKDIR /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /build/cmd/server .

# Copy the /tmp directory (with permissions) from the tmp_maker stage
COPY --from=builder /tmp /tmp

# run server
EXPOSE $SERVICE_PORT
CMD ["./server"]