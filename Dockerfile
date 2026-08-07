FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG PACKAGE=./services/order/cmd/order-service
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/service ${PACKAGE}

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/service /app/service
COPY services/*/etc/*.yaml /app/config/
COPY deploy/config /app/deploy/config
COPY migrations /app/migrations

USER 65532:65532
ENTRYPOINT ["/app/service"]
