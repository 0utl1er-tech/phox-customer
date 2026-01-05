FROM golang:1.24-alpine AS builder

RUN apk update && apk add --no-cache git
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o /app/tmp/main /app/main.go


FROM alpine:3.19

RUN apk update && apk add --no-cache ca-certificates curl

# Install golang-migrate
RUN curl -L https://github.com/golang-migrate/migrate/releases/download/v4.17.0/migrate.linux-amd64.tar.gz | tar xvz && \
    mv migrate /usr/bin/migrate

WORKDIR /root/

COPY --from=builder /app/tmp/main .
COPY app.env .
COPY db/migration ./migration
COPY start.sh .

ENV ENV=release

ENTRYPOINT ["/root/start.sh"]
CMD ["./main"]