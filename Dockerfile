ARG DUMB_INIT_VERISON="1.2.5"
ARG DUMB_INIT_URL="https://github.com/Yelp/dumb-init/releases/download/v${DUMB_INIT_VERISON}/dumb-init_${DUMB_INIT_VERISON}_x86_64"

ARG CGO_ENABLED=0
ARG GOOS=linux
ARG GOARCH=amd64
ARG GO_LDFLAGS="-s -w"


FROM golang:1.25.6-alpine3.23 AS build
ARG CGO_ENABLED
ARG GOOS
ARG GOARCH
ARG GO_BUILD_ARGS

COPY src /src

WORKDIR /src/ninit
RUN go build -ldflags="${GO_LDFLAGS}" .

WORKDIR /src/ninit-supervise
RUN go build -ldflags="${GO_LDFLAGS}" .


FROM alpine:3.23
ARG DUMB_INIT_URL


RUN <<EOF
set -xe

apk add --no-cache \
    docker \
    docker-cli \
    docker-cli-buildx \
    docker-cli-compose
EOF

ADD --link=true --chmod=755 ${DUMB_INIT_URL} /sbin/dumb-init
COPY --link=true --from=build /src/ninit/ninit /sbin/ninit
COPY --link=true --from=build /src/ninit-supervise/ninit-supervise /sbin/ninit-supervise
COPY --link=true ./etc /etc

HEALTHCHECK --interval=1s --timeout=3s --start-period=5s --retries=500 \
    CMD test -f /var/run/ninit.ok

ENTRYPOINT [ "/sbin/dumb-init", "--single-child", "--", "/sbin/ninit" ]
CMD [ "/bin/sh" ]
