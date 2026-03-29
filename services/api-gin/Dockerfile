FROM --platform=linux/amd64 golang:1.25-alpine AS builder
WORKDIR /src

RUN apk add --no-cache git

COPY go.mod .
RUN go mod tidy

COPY . .
RUN go build -o /usr/local/bin/api-gin ./...

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /usr/local/bin/api-gin /usr/local/bin/api-gin
EXPOSE 8000
ENTRYPOINT ["/usr/local/bin/api-gin"]
