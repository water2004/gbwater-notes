# Linux 部署与运维

宿主机 nginx 直接读取静态目录，保留原始请求 Host，把 `/api/` 转发给仅在环回地址监听的 Go 容器。Compose 自动为三个服务组网，持久化目录都在 `docker-compose.yml` 所在目录下。

## 首次启动

在 Linux 主机上安装 Docker Engine、Compose 插件和 nginx。使用仓库 `main` 分支中的 [Compose 配置](../docker-compose.yml) 和 `deploy/`，镜像版本由 `.env` 指定。

```bash
cp deploy/compose.env.example .env
```

在 `.env` 填写真实的 HTTPS 站点地址、管理员邮箱及独立密码。数据库密码和搜索密钥可各执行一次 `openssl rand -hex 24`。

```bash
mkdir -p data/db data/meilisearch
sudo install -d -m 0755 -o 10001 -g 10001 content frontend data/gbwater data/gbwater/content-assets data/render
docker compose pull
docker compose up -d --wait db search
docker inspect -f '{{range .NetworkSettings.Networks}}{{.Gateway}}{{end}}' "$(docker compose ps -q db)"
```

把上面查到的网关填入 `.env` 的 `GBWATER_DOCKER_GATEWAY`，API 据此识别宿主机 nginx 转发的访客 IP。

```bash
docker compose run --rm --no-deps api export-frontend
docker compose up -d --wait
```

`export-frontend` 将初始页面写入空的 `frontend/`。已有发布目录可以直接交给 nginx。运行容器的 UID/GID 是 `10001`，导入的内容目录和图床也需要该用户可读写。

使用 [nginx 示例](../deploy/nginx.conf.example)，填写域名、证书、实际部署目录和 API 端口。应用接收生产环境的 Secure Cookie，从 HTTPS 站点登录后台。

## 修改和发布内容

1. 登录站点后台。新建笔记，或导入自己的 Markdown 目录及图床文件。
2. 从「发布」点击「构建并发布」。容器内的 Go 服务调用 Linux Folia 生成 HTML、公式与图表，再运行 Vite SSG。宿主机 nginx 随即读取更新后的 `frontend/`。
3. 访问站点首页、笔记与 `/api/health` 确认。修改目录、学科或站点文案后使用同一个发布入口。

随本镜像的初始页面使用 `/mypdf/`。自定义挂载路径时，在带子模块的源码目录执行 `docker build --build-arg GBWATER_BASE_PATH=/your-path/ -t gbwater-notes:custom .`，同步设置 `GBWATER_IMAGE`、`GBWATER_BASE_PATH` 和 nginx 的 location。

## 环境变量

完整模板见 [Compose 环境配置](../deploy/compose.env.example)。本地开发的 [环境配置](../.env.example) 连接宿主机数据库。

| 变量 | 用途 |
| --- | --- |
| `GBWATER_IMAGE` | Linux 镜像版本，建议使用具体版本或镜像摘要 |
| `GBWATER_SITE_URL`、`GBWATER_BASE_PATH` | 站点外部 HTTPS 地址和前端挂载路径 |
| `GBWATER_PORT` | 宿主机环回 API 端口 |
| `GBWATER_DB_PASSWORD`、`GBWATER_MEILISEARCH_KEY` | 数据库密码与索引服务的独立密钥 |
| `GBWATER_ADMIN_USER/EMAIL/PASSWORD` | 在空数据库中引导管理员 |
| `GBWATER_ALLOW_REGISTRATION` | 公开注册开关 |
| `GBWATER_MAIL_MODE` | `smtp` 或 `off` |
| `GBWATER_SMTP_ADDR/USER/PASSWORD/FROM/TLS` | SMTP 地址、账号、密码、发件地址和 TLS 模式 |
| `GBWATER_DOCKER_GATEWAY` | Docker 分配给本 Compose 项目的网关地址，用于信任宿主机 nginx 的转发头 |

需要开放注册或找回密码时，配置 SMTP 后再启用相应功能。用 587 端口时 TLS 通常填 `starttls`，465 通常填 `tls`。注册域名限制和认证限流在后台保存，配置保存在 PostgreSQL。

## 数据和升级

| 路径或卷 | 内容 |
| --- | --- |
| `content/` | Markdown 原文 |
| `data/gbwater/content-assets/` | 图床原图 |
| `data/gbwater/asset-trash/` | 图片回收站 |
| `data/render/` | 可重建的后台渲染数据 |
| `frontend/` | nginx 读取的静态发布目录 |
| `data/db/` | PostgreSQL 数据，包含账号、会话、划线便签、站点设置和 Markdown 修订 |
| `data/meilisearch/` | 可重建的搜索索引 |

从最初使用命名卷的 `v1.0.0` Compose 迁移时，先从原 PostgreSQL 导出并验证备份，再恢复到 `data/db/` 对应的数据库。切换前保留原命名卷。搜索索引可在后台重新构建。

升级前备份 PostgreSQL 和内容、图床、回收站。调整 `GBWATER_IMAGE` 后执行 `docker compose pull && docker compose up -d --wait`，然后从后台重新构建页面。首次导出只用于初始化静态目录。

排查时可用 `docker compose ps`、`docker compose logs api`、`curl http://127.0.0.1:8080/api/health` 和 `nginx -t`。Folia 与前端构建日志可在后台「发布」查看。
