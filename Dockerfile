# =============================================================================
# Stage 1: Builder
# =============================================================================
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /app/server \
    .

# =============================================================================
# Stage 2: Runtime
# =============================================================================
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

RUN adduser -D -g '' appuser

WORKDIR /app

COPY --from=builder /app/server .

COPY app.env .

RUN chown -R appuser:appuser /app

USER appuser

EXPOSE 8082

# Environment variables for runtime configuration
# These can be overridden at runtime via docker run -e or docker-compose
#
# Database configuration
# ENV DB_SOURCE=postgresql://user:password@host:5432/dbname?sslmode=disable
#
# Server configuration
# ENV CONNECT_SERVER_ADDRESS=:8082
#
# JWT/Firebase configuration (for token validation - uses public JWKS URL)
# ENV JWT_ENABLED=true
# ENV JWT_PROJECT_ID=your-firebase-project-id
# ENV JWT_ISSUER_URL=https://securetoken.google.com/your-firebase-project-id
# ENV JWT_JWKS_URL=https://www.googleapis.com/service_accounts/v1/jwk/securetoken@system.gserviceaccount.com
#
# Firebase Admin SDK credentials (optional - only needed for CreateCompanyUser)
# Mount the credentials file and set the path:
#   docker run -v /path/to/credentials.json:/app/credentials.json \
#              -e FIREBASE_ADMIN_CREDENTIALS_FILE=/app/credentials.json ...
# ENV FIREBASE_ADMIN_CREDENTIALS_FILE=/app/credentials.json

CMD ["./server"]
