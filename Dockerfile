FROM golang:1.21-alpine AS builder

WORKDIR /app

# کپی فایل‌های go.mod و go.sum
COPY go.mod go.sum ./
RUN go mod download

# کپی کد منبع
COPY . .

# Build برنامه
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# مرحله نهایی
FROM alpine:latest

RUN apk --no-cache add ca-certificates wget

WORKDIR /root/

# کپی برنامه از builder
COPY --from=builder /app/main .

EXPOSE 8080

CMD ["./main"]