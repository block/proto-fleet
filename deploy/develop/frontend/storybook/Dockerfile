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

RUN source bin/activate-hermit && npm run build-storybook

FROM nginx:1.27.5-alpine

WORKDIR /app

COPY --from=build /build/storybook-static /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
