FROM golang:1.21-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -tags netgo -ldflags '-s -w' -o app

FROM alpine:latest
WORKDIR /app
COPY --from=build /app/app .
COPY --from=build /app/web ./web
EXPOSE 8080
CMD ["./app"]