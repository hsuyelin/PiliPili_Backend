# Use multi-stage build; first stage is for compilation
FROM golang:1.23.5-alpine3.21 AS builder

# Set working directory
WORKDIR /app

# Copy dependency files and download them
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire codebase
COPY . .

# Compile the binary, disable CGO, and optimize for size
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o emby_backend main.go

# Second stage, using a minimal alpine image as the base
FROM alpine:3.21

# Set working directory
WORKDIR /app

# Copy the compiled binary from the builder stage
COPY --from=builder /app/emby_backend /app/emby_backend
# Copy the configuration file
COPY config.yaml /app/config.yaml

# Set the default command to run the binary
CMD ["/app/emby_backend", "/app/config.yaml"]
