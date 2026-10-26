FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/task-service ./cmd/api

FROM alpine:3.20
RUN adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/task-service ./task-service
COPY docs ./docs
USER app
EXPOSE 8080
ENTRYPOINT ["./task-service"]
