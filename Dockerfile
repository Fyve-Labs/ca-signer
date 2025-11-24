FROM --platform=$BUILDPLATFORM golang:alpine AS builder

WORKDIR /app

# Download Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source files
COPY main.go ./

RUN apk add --no-cache \
    unzip \
    ca-certificates

RUN update-ca-certificates

# Build
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOGC=75 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags "-w -s" -o /ca-signer main.go

# ? -------------------------
FROM scratch

COPY --from=builder /ca-signer /
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

EXPOSE 4443

ENV PROVISIONER_NAME="autocert"

ENTRYPOINT ["/ca-signer"]

CMD ["/etc/ca-signer/config.yaml"]
