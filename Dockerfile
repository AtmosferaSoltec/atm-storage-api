FROM golang:1.27.3-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /api ./cmd/api

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && addgroup -S storage && adduser -S -G storage storage
WORKDIR /app
COPY --from=build /api /app/api
USER storage
EXPOSE 8084
ENTRYPOINT ["/app/api"]
CMD ["serve"]
