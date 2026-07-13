ARG MYSQL_IMAGE=mysql:8.4.10

FROM ${MYSQL_IMAGE}

COPY --chmod=0644 scripts/mysql_init.sh \
  /docker-entrypoint-initdb.d/000_mysql_init.sh
COPY --chmod=0644 database/mysql/init/ \
  /docker-entrypoint-initdb.d/
