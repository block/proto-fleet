FROM debian:12.10 AS build

WORKDIR /build

SHELL ["/bin/bash", "-c"]

RUN apt-get update && apt-get install -y \
    git \
    curl

COPY bin bin/

RUN chmod +x bin/activate-hermit

COPY client/package*.json ./

RUN source bin/activate-hermit && npm clean-install

COPY client ./

RUN source bin/activate-hermit && npm run build:protoOS

FROM protofleet/miner-sim-develop-base:latest

COPY --from=build /build/dist/protoOS /app/miner-web/dist/protoOS
