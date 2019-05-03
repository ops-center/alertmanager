# stage 1: build
FROM golang:1.10-alpine AS builder

# Add source code
RUN mkdir -p /go/src/github.com/searchlight/alertmanager
ADD . /go/src/github.com/searchlight/alertmanager

# Build binary
RUN cd /go/src/github.com/searchlight/alertmanager && \
    GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /go/bin/alertmanager

# stage 2: lightweight "release"
FROM alpine:latest

COPY --from=builder /go/bin/alertmanager /bin/

ENTRYPOINT [ "/bin/alertmanager" ]
