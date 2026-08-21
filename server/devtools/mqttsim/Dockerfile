FROM golang:1.26.5-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY devtools/mqttsim ./devtools/mqttsim
RUN go build -o /out/mqttsim ./devtools/mqttsim

FROM alpine:3.22

COPY --from=build /out/mqttsim /usr/local/bin/mqttsim

EXPOSE 4183
ENTRYPOINT ["mqttsim"]
