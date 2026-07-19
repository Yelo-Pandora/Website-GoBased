ARG NODE_IMAGE=node:24.18.0-alpine3.23
ARG NGINX_IMAGE=nginx:1.30.3-alpine3.23

FROM ${NODE_IMAGE} AS assets

WORKDIR /app
COPY deploy/swagger-ui/package.json deploy/swagger-ui/package-lock.json ./
RUN npm ci

FROM ${NGINX_IMAGE}

COPY deploy/nginx/edge/nginx.conf /etc/nginx/nginx.conf
COPY deploy/swagger-ui/default.conf /etc/nginx/conf.d/default.conf
COPY deploy/swagger-ui/index.html deploy/swagger-ui/swagger-initializer.js /usr/share/nginx/html/
COPY contracts/http/platform-api.openapi.yaml /usr/share/nginx/html/platform-api.openapi.yaml
COPY --from=assets /app/node_modules/swagger-ui-dist/swagger-ui.css /usr/share/nginx/html/assets/swagger-ui.css
COPY --from=assets /app/node_modules/swagger-ui-dist/swagger-ui-bundle.js /usr/share/nginx/html/assets/swagger-ui-bundle.js

RUN chown -R nginx:nginx /etc/nginx /usr/share/nginx/html /var/cache/nginx

USER nginx
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=5 \
  CMD wget -q -O - http://127.0.0.1:8080/healthz >/dev/null || exit 1
