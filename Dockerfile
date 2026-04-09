# Stage 1: build the static binary.
FROM golang:1.23-alpine AS builder

WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" \
    -o /out/neospec \
    ./cmd/neospec

# Stage 2: minimal runtime image.
# We use alpine (not scratch) because Neovim's download requires CA certificates
# for TLS verification, and we want a shell for debugging in CI.
FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=builder /out/neospec /usr/local/bin/neospec

ENTRYPOINT ["/usr/local/bin/neospec"]
CMD ["run"]
