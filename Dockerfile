# Gunakan image base Golang
FROM golang:alpine
#FROM golang:1.21 AS builder

# Install git for dependency management
RUN apk update && apk add --no-cache git

# Set working dir in container
WORKDIR /app


COPY . /app

# Download dependencies
RUN go mod tidy 

# Build binary
RUN go build -o monitoring_service

# Run  app when started
ENTRYPOINT ["/app/monitoring_service"]

