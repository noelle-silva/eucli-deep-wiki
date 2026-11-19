# 只构建并承载服务端 eucli-deep-wiki-server。
# 命令行 eucli-dw 不进镜像：它在本地单独构建分发，镜像里也不装任何与它相关的东西。
FROM golang:1.25-alpine AS build

# CGO_ENABLED=0：产物是静态单文件，落到运行层不再依赖编译环境。
# GOWORK=off：镜像里只有服务端模块，构建不读工作区文件，也不看另一侧的源码。
ENV CGO_ENABLED=0 \
    GOWORK=off

WORKDIR /src

COPY server/go.mod ./
COPY server/cmd ./cmd
COPY server/internal ./internal

RUN go build -trimpath -o /out/eucli-deep-wiki-server ./cmd/eucli-deep-wiki-server

FROM alpine:3.22

# 运行层只装服务端自己要用的东西：证书包（访问远端仓库）、版本管理工具、
# 加密传输客户端（SSH 形式的远端）、时区数据。
RUN apk add --no-cache ca-certificates git openssh-client tzdata \
    && git config --system core.longpaths true \
    && git config --system safe.directory '*'

WORKDIR /app

COPY --from=build /out/eucli-deep-wiki-server /usr/local/bin/eucli-deep-wiki-server

# 端口只属于服务端：配置文件里的 httpAddress 与编排里的端口映射都指向这里。
EXPOSE 9997

# 入口就是服务端程序本身，参数由它自己解析；它没有任何子命令。
ENTRYPOINT ["eucli-deep-wiki-server"]
CMD ["-config", "/app/config.json"]
