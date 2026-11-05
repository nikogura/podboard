# Build the UI
FROM node:18-alpine AS ui-builder

WORKDIR /app/ui
COPY pkg/ui/package*.json ./
RUN npm ci

COPY pkg/ui/ ./
RUN npm run build

# Build the Go binary
FROM golang:1.24-alpine AS go-builder

# Install git and openssh for private repo access
RUN apk add --no-cache git openssh

# Configure git for SSH access to private repos
RUN git config --global url."git@github.com:".insteadOf "https://github.com/"
RUN mkdir ~/.ssh
RUN ssh-keyscan -H github.com >> ~/.ssh/known_hosts

WORKDIR /app
COPY go.mod go.sum ./
# Remove the executor dependency and replace directive for Docker build
RUN sed -i '/github.com\/subdialia\/executor/d' go.mod
RUN --mount=type=ssh go mod download

COPY . .
# Copy the built UI into the Go embed directory
COPY --from=ui-builder /app/ui/dist ./pkg/ui/dist

RUN --mount=type=ssh go build

# Final runtime image
FROM alpine:3.19

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Create the non-root runtime user before anything references it.
RUN addgroup -g 1001 -S podboard && \
    adduser -u 1001 -S podboard -G podboard

# The binary lives in a world-searchable directory, root-owned and
# world-executable, so it runs under the image's own user AND under any
# arbitrary uid a Pod securityContext assigns (the k8s manifests use 65534).
# It must never sit under /root, which is mode 0700.
WORKDIR /app
COPY --from=go-builder --chmod=0755 /app/podboard /app/podboard

USER podboard
ENV HOME=/home/podboard

EXPOSE 9999

CMD ["/app/podboard"]