# --- build stage ---
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/controller ./cmd/controller

# --- run stage ---
FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/controller /app/controller
COPY configs /app/configs
EXPOSE 8080
ENV ADDR=:8080
ENTRYPOINT ["/app/controller"]
