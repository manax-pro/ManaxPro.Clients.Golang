# Dockerfile for manax-go
#
# Usage (local):
#   docker build -t manax-go .
#   docker run --rm manax-go
#
# By default, container runs "go test ./...".

FROM golang:1.22-bullseye AS build

WORKDIR /app

# Enable Go module mode explicitly.
ENV GO111MODULE=on

# Copy go.mod and go.sum first to leverage Docker layer caching.
COPY go.mod ./
RUN go mod download

# Copy the rest of the source code.
COPY . .

# Run tests at build time (optional but useful in CI).
RUN go test ./...

# Build a small CLI example for debugging.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /usr/local/bin/manax-go-example ./cmd/manax-go-example

# Final runtime image: small, but still with shell for debugging.
FROM gcr.io/distroless/base-debian11

WORKDIR /app

COPY --from=build /usr/local/bin/manax-go-example /usr/local/bin/manax-go-example

# Default command runs example (can be overridden in docker run).
ENTRYPOINT ["/usr/local/bin/manax-go-example"]
