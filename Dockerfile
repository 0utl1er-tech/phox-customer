FROM golang:1.23.0-alpine3.19 AS builder

RUN apk update && apk add --no-cache git
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o /app/tmp/main /app/cmd/main.go


FROM alpine:3.19

RUN apk update && apk add --no-cache ca-certificates

WORKDIR /root/

COPY --from=builder /app/tmp/main .

ENV ENV=release

CMD ["./main"]