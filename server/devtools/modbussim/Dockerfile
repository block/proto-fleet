FROM golang:1.26.5-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./

COPY devtools/modbussim ./devtools/modbussim
RUN go build -o /out/modbussim ./devtools/modbussim

FROM alpine:3.22

COPY --from=build /out/modbussim /usr/local/bin/modbussim

EXPOSE 5502
ENTRYPOINT ["modbussim"]
