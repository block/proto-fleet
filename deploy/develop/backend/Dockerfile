FROM golang:1.25.2-alpine AS build

WORKDIR /build

COPY server/go.mod server/go.sum ./

RUN go mod download

COPY server ./

RUN CGO_ENABLED=0 go build -v ./cmd/fleetd

FROM alpine:3.21.3

WORKDIR /app

COPY --from=build /build/fleetd ./fleetd

EXPOSE 80

ENTRYPOINT ["./fleetd"]
