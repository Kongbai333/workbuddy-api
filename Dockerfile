# workbuddy-api · 源码构建镜像（干净 clone 下直接 `docker build .` 即可）
#
# 与 docker/Dockerfile 的分工（两个都保留，各有用途）：
#   ├─ 本文件（仓库根）：多阶段构建，**镜像内自己编译 Go 源码**。
#   │                    构建上下文 = 仓库根目录 → 不需要事先交叉编译二进制。
#   │                    根目录 docker-compose.yml 与 CI（.github/workflows/docker.yml）
#   │                    都走这条路径。
#   └─ docker/Dockerfile：只 COPY 开发机交叉编译好的静态二进制，镜像内零编译零拉取。
#                        用于「NAS/弱网现场构建」与「离线镜像 tar」两条老路径
#                        （docker/build-on-nas.sh、docker/build-image.sh）。
#
# 运行阶段为何仍是 alpine + 零 apk 依赖：见 docker/Dockerfile 顶部注释 ——
# tzdata 已内嵌进二进制（import _ "time/tzdata"）、CA 证书用 minirootfs 自带、
# sh/wget 由 busybox 提供，因此构建与运行全程不需要任何包管理器联网。

# ---------- 构建阶段 ----------
# --platform=$BUILDPLATFORM：构建阶段固定跑在**构建机的原生架构**上，
# 再用 GOARCH 交叉编译到目标架构。这样多架构构建（--platform linux/amd64,linux/arm64）
# 不需要 QEMU 模拟整条编译链，速度快一个量级。
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

# 先只拷 go.mod/go.sum 再 download：改源码不会让「依赖层」的缓存失效，
# 重复构建时这一层直接命中缓存（这是镜像构建里最值钱的一次分层）。
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 → 纯静态链接，运行镜像不需要 glibc 或任何额外运行库。
# -trimpath      → 不把构建机上的绝对路径带进产物（同时让不同机器的构建可复现）。
# -s -w          → 去掉符号表与调试信息，体积约减 25%。
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w" -o /out/workbuddy-api ./cmd/workbuddy-api

# ---------- 运行阶段 ----------
FROM alpine:3.20

WORKDIR /app

COPY --from=builder /out/workbuddy-api /app/workbuddy-api
COPY docker/entrypoint.sh /app/entrypoint.sh

# 顺手把 CRLF 清掉：在 Windows 上直接 docker build 时，若工作区里的脚本被检出成
# CRLF，容器内 /bin/sh 会报 "bad interpreter: No such file or directory" 或莫名其妙的
# 语法错误 —— 文件在编辑器里看着完全正常，排查成本很高。（.gitattributes 已声明
# *.sh eol=lf，这里是针对「未走 git 的构建上下文」的兜底。）
RUN sed -i 's/\r$//' /app/entrypoint.sh \
    && chmod +x /app/workbuddy-api /app/entrypoint.sh

# 只把「数据目录」声明为卷。**绝不声明或挂载单个文件** ——
# 把 config.json 作为单文件 bind 挂载时，宿主上该文件不存在，Docker 会建出
# 一个同名**空目录**，程序读配置即崩（现象是容器 Exited:0）。
VOLUME ["/data"]
EXPOSE 7863

# 配置全部通过 WILDWORK_* 环境变量传入，由 entrypoint 在容器内生成 config.json。
ENV WILDWORK_HOME="/app" \
    WILDWORK_LISTEN=":7863" \
    WILDWORK_AUTH_DIR="/data/auths" \
    WILDWORK_STATE_FILE="/data/state.json" \
    WILDWORK_REGION="cn" \
    TZ="Asia/Shanghai"

# /healthz 无需鉴权，探针不带 API Key。
HEALTHCHECK --interval=30s --timeout=5s --retries=3 --start-period=15s \
    CMD wget -q -O - http://127.0.0.1:7863/healthz || exit 1

ENTRYPOINT ["/app/entrypoint.sh"]
