ARG GO_IMAGE=golang:1.26.5-alpine3.23
ARG ALPINE_IMAGE=alpine:3.23.5

FROM ${GO_IMAGE} AS build

ARG BUILD_COMMIT=unknown
ARG BUILD_TIME=unknown
ARG BUILD_VERSION=dev
ARG GOPROXY=https://goproxy.cn,direct

WORKDIR /src
COPY go.mod go.sum ./
RUN GOPROXY="${GOPROXY}" go mod download
COPY internal ./internal
COPY services/lab-app ./services/lab-app
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w \
      -X website-gobased/internal/version.Version=${BUILD_VERSION} \
      -X website-gobased/internal/version.Commit=${BUILD_COMMIT} \
      -X website-gobased/internal/version.BuildTime=${BUILD_TIME}" \
    -o /out/lab-app \
    ./services/lab-app/cmd/lab-app

FROM ${ALPINE_IMAGE}

RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 platform \
    && adduser -S -D -H -u 10001 -G platform platform

COPY --from=build /out/lab-app /usr/local/bin/lab-app

USER platform
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/lab-app"]
