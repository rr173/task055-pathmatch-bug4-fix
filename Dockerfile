# syntax=docker/dockerfile:1

FROM docker.m.daocloud.io/library/golang:1.26.3-bookworm AS build
WORKDIR /src
COPY . .
ENV GOTOOLCHAIN=local \
    GOPROXY=https://goproxy.cn,direct \
    GOSUMDB=sum.golang.google.cn \
    CGO_ENABLED=0
RUN go build -mod=vendor -o /out/pathmatch .

FROM docker.m.daocloud.io/library/alpine:3.20
COPY --from=build /out/pathmatch /usr/local/bin/pathmatch
ENTRYPOINT ["/usr/local/bin/pathmatch"]
CMD ["--smoke-test"]
