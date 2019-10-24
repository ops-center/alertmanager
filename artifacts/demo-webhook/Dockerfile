# stage 1: build
FROM golang:1.10-alpine AS builder

# Add source code
RUN mkdir -p /go/src/searchlight.dev/alertmanager/artifacts/demo-webhook
ADD . /go/src/searchlight.dev/alertmanager/artifacts/demo-webhook

# Build binary
RUN cd /go/src/searchlight.dev/alertmanager/artifacts/demo-webhook && \
    GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /go/bin/webhook

# stage 2: lightweight "release"
FROM alpine:latest

COPY --from=builder /go/bin/webhook /bin/

ENTRYPOINT [ "/bin/webhook"]
