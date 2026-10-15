# workbuddy-api 极简运行镜像。
#
# ★ 想「从源码直接构建」请看**仓库根目录**的 ../Dockerfile（多阶段，镜像内编译），
#   本文件是它的**离线孪生版**：要求先把二进制交叉编译好放在本目录。
#   两者分工见 docker/README.md「二、四条部署路径」。
#
# 设计原则：镜像内**不编译、不拉依赖**，只 COPY 开发机交叉编译好的单个静态二进制 ——
# 彻底规避在 NAS 上 go mod download 访问 proxy.golang.org 超时（i/o timeout）。
# 那是同类项目在容器里现场构建时最常见的失败点。
#
# 二进制 workbuddy-api 由开发机交叉编译（GOOS=linux GOARCH=amd64 CGO_ENABLED=0）：
# 单进程 = OpenAI 兼容 HTTP 服务 + 双调度器 + 静态 Web 管理面板；
# 容器内以 --no-tray 无头运行（容器无桌面环境）。
FROM alpine:3.20

# ★ 零 apk 依赖（全程不做网络拉取，离线也能构建）：
#   ca-certificates → minirootfs 已内置 /etc/ssl/certs/ca-certificates.crt（访问上游 HTTPS 必需）
#   sh / ash / wget → busybox 自带
#   tzdata          → **已内嵌进二进制**（import _ "time/tzdata"），无需系统提供 zoneinfo。
#                     若缺它，TZ=Asia/Shanghai 无法解析、time.Local 静默退化为 UTC，
#                     定时签到会按 UTC 时刻触发（北京时间 17:00/05:00 而非 09:00/21:00）。
#   jq              → 不需要：账号登录走 Web 面板（/api/login/start），容器内无需 CLI 助手。

WORKDIR /app

COPY workbuddy-api /app/workbuddy-api
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/workbuddy-api /app/entrypoint.sh

# 只把「数据目录」声明为卷。**绝不声明或挂载单个文件** ——
# 历史上曾把 config.json 作为单文件 bind 挂载，当宿主上该文件不存在时
# Docker 会建出一个同名**空目录**，程序读配置即崩（现象是容器 Exited:0）。
VOLUME ["/data"]
# 17863 = 登录回调端口（TraeWork / 千问办公）。容器里 127.0.0.1 指的是容器自己，
# 回调必须走「映射到宿主机」的固定端口，否则授权完成后面板一直停在「等待完成」。
EXPOSE 7863 17863

# 配置全部通过 WILDWORK_* 环境变量传入，由 entrypoint 在容器内生成 config.json。
# WILDWORK_LOGIN_CALLBACK **故意不设默认值**：设了就等于「每次启动都覆盖」，
# 用户在面板里改的地址会在下次 up -d 后丢失。默认值（http://127.0.0.1:17863）
# 由 entrypoint 兜底，取值优先级见 entrypoint.sh。
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
