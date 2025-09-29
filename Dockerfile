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

WORKDIR /root/

# Copy the binary
COPY --from=go-builder /app/podboard .

# Create non-root user
RUN addgroup -g 1001 -S podboard && \
    adduser -u 1001 -S podboard -G podboard

USER podboard

EXPOSE 9999

CMD ["./podboard"]