FROM golang:1.22-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY server/ ./server/
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o c2server ./server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates sqlite-libs tzdata
WORKDIR /app
COPY --from=builder /app/c2server .
RUN mkdir -p /var/data
ENV PORT=8080 DB_PATH=/var/data/c2.db
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://localhost:8080/healthz || exit 1
CMD ["./c2server"]
