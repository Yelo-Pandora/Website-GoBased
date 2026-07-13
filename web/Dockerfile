ARG NODE_IMAGE=node:24.18.0-alpine3.23
ARG NGINX_IMAGE=nginx:1.30.3-alpine3.23

FROM ${NODE_IMAGE} AS build

WORKDIR /app
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/index.html web/vite.config.js ./
COPY web/src ./src
RUN npm run build

FROM ${NGINX_IMAGE}

COPY deploy/nginx/edge/nginx.conf /etc/nginx/nginx.conf
COPY deploy/nginx/edge/default.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html

RUN chown -R nginx:nginx /etc/nginx /usr/share/nginx/html /var/cache/nginx

USER nginx
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=5 \
  CMD wget -q -O - http://127.0.0.1:8080/healthz >/dev/null || exit 1
