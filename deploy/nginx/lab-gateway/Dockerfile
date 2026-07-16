ARG NGINX_IMAGE=nginx:1.30.3-alpine3.23

FROM ${NGINX_IMAGE}

COPY deploy/nginx/lab-gateway/nginx.conf /etc/nginx/nginx.conf
COPY deploy/nginx/lab-gateway/static /etc/nginx/static
COPY deploy/nginx/lab-gateway/templates /opt/platform/nginx-templates

RUN mkdir -p /etc/nginx/labs \
    && chown -R nginx:nginx /etc/nginx /opt/platform /var/cache/nginx \
    && chown 10001:10001 /etc/nginx/labs \
    && chmod 0755 /etc/nginx/labs

USER nginx
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=5 \
  CMD wget -q -O - http://127.0.0.1:8080/healthz >/dev/null || exit 1
