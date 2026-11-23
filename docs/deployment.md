# Linux 部署与运维

宿主机 nginx 直接读取静态目录，把公开 API 前缀（默认 `/mypdf/api/`）转发到 Go 容器内部的 `/api/`。Compose 自动为三个服务组网，持久化目录都在 `docker-compose.yml` 所在目录下。

## 首次启动

在 Linux amd64 主机上安装 Docker Engine、Compose 插件和 nginx。以下命令把 v1.0.1 的部署文件取到当前目录下的 `gbwater-notes/`。Compose 直接拉取已构建的版本镜像。

```bash
git clone --depth 1 --branch v1.0.1 https://github.com/water2004/gbwater-notes.git
cd gbwater-notes
cp deploy/compose.env.example .env
```

在 `.env` 填写真实的 HTTPS 站点地址、管理员邮箱及独立密码。数据库密码和搜索密钥可各执行一次 `openssl rand -hex 24`。首次启动时 Go 服务按 `GBWATER_ADMIN_*` 自动创建管理员。已有管理员时，继续使用数据库里保存的账号和密码。

```bash
mkdir -p data/db data/meilisearch
sudo install -d -m 0755 -o 10001 -g 10001 content frontend data/gbwater data/gbwater/content-assets data/render
docker compose pull
docker compose up -d --wait db search
docker inspect -f '{{range .NetworkSettings.Networks}}{{.Gateway}}{{end}}' "$(docker compose ps -q db)"
```

把上面查到的网关填入 `.env` 的 `GBWATER_DOCKER_GATEWAY`，API 据此识别宿主机 nginx 转发的访客 IP。

```bash
docker compose up -d --wait
```

API 容器启动时会把镜像里的登录、注册、找回密码和后台页面安装到 `frontend/`，并按 `.env` 替换路径。同一配置重启时会跳过内容相同的文件。运行容器的 UID/GID 是 `10001`，导入的内容目录和图床也需要该用户可读写。

将 [nginx 示例](../deploy/nginx.conf.example) 加到宿主机 nginx 配置中，换成实际域名、证书、`frontend/` 绝对路径和 API 端口。默认外部 API 前缀为 `/mypdf/api/`，示例中的 `proxy_pass` 末尾保留 `/api/`，由 nginx 将外部前缀改写为 Go 内部的路由。笔记图片和构建后的公式、图表由 nginx 直接读取。

```bash
sudo nginx -t && sudo systemctl reload nginx
curl -fsS https://notes.example.com/mypdf/api/health
```

健康检查通过后，从 HTTPS 的 `/mypdf/admin/` 进入后台，再点击「构建并发布」生成公开首页和笔记。

## 修改和发布内容

1. 登录站点后台。新建笔记，或导入自己的 Markdown 目录及图床文件。
2. 从「发布」点击「构建并发布」。容器内的 Go 服务调用 Linux Folia 生成 HTML、公式与图表，再运行 Vite SSG。宿主机 nginx 随即读取更新后的 `frontend/`。
3. 访问站点首页、笔记与公开 API 前缀下的 `health`（如 `/mypdf/api/health`）确认。修改目录、学科或站点文案后使用同一个发布入口。

改路径时调整 `.env` 的 `GBWATER_BASE_PATH` 和 `GBWATER_API_BASE_PATH`、同步修改 nginx 的 location，再运行 `docker compose up -d --wait`。同一镜像即可启动新的登录和后台路径。公开页由管理员随后点击「构建并发布」，服务按新路径更新对应的静态产物。

## 环境变量

完整模板见 [Compose 环境配置](../deploy/compose.env.example)。本地开发的 [环境配置](../.env.example) 连接宿主机数据库。

| 变量 | 用途 |
| --- | --- |
| `GBWATER_IMAGE` | Linux 镜像版本，建议使用具体版本或镜像摘要 |
| `GBWATER_SITE_URL`、`GBWATER_BASE_PATH` | 站点外部 HTTPS 地址和前端挂载路径 |
| `GBWATER_API_BASE_PATH` | 公开 API 路径，留空时为前端挂载路径加 `api/` |
| `GBWATER_PORT` | 宿主机环回 API 端口 |
| `GBWATER_DB_PASSWORD`、`GBWATER_MEILISEARCH_KEY` | 数据库密码与索引服务的独立密钥 |
| `GBWATER_ADMIN_USER/EMAIL/PASSWORD` | 在空数据库中引导管理员 |
| `GBWATER_ALLOW_REGISTRATION` | 公开注册开关 |
| `GBWATER_MAIL_MODE` | `smtp` 或 `off` |
| `GBWATER_SMTP_ADDR/USER/PASSWORD/FROM/TLS` | SMTP 地址、账号、密码、发件地址和 TLS 模式 |
| `GBWATER_DOCKER_GATEWAY` | Docker 分配给本 Compose 项目的网关地址，用于信任宿主机 nginx 的转发头 |

需要开放注册或找回密码时，配置 SMTP 后再启用相应功能。用 587 端口时 TLS 通常填 `starttls`，465 通常填 `tls`。注册域名限制和认证限流在后台保存，配置保存在 PostgreSQL。

## 数据和升级

| 路径 | 内容 |
| --- | --- |
| `content/` | Markdown 原文 |
| `data/gbwater/content-assets/` | 图床原图 |
| `data/gbwater/asset-trash/` | 图片回收站 |
| `data/render/` | 可重建的后台渲染数据 |
| `frontend/` | nginx 读取的静态发布目录 |
| `data/db/` | PostgreSQL 数据，包含账号、会话、划线便签、站点设置和 Markdown 修订 |
| `data/meilisearch/` | 可重建的搜索索引 |

### 升级到 v1.0.1

在原部署目录操作。备份 PostgreSQL、`.env`、内容、图床和回收站，保留已有的 `content/`、`data/gbwater/`、`data/render/` 和 `frontend/`。如果原数据库已经挂载到 `./data/db/`，可继续使用。使用了 v1.0.0 原始命名卷时，先执行下方迁移步骤。

1. 用 [v1.0.1 Compose 配置](../docker-compose.yml) 更新现有配置，保留部署时自定义的目录和端口。在原 `.env` 将 `GBWATER_IMAGE` 改为 `ghcr.io/water2004/gbwater-notes:1.0.1`，设置 `GBWATER_API_BASE_PATH` 或留空使用默认值。
2. 执行 `docker compose pull`、`docker compose up -d --wait db search`。按首次启动步骤重新查网关并更新 `.env`，再执行 `docker compose up -d --wait`。启动后的登录和后台页面会使用新路径。
3. 同步更新 nginx 的 API location 和 `admin-assets/` 配置，检查配置并重载。通过 HTTPS 登录后台，点击「构建并发布」。这一步会更新公开页的 API 路径，并计算已有划线、书签和便签在当前正文中的位置。完成后刷新旧阅读页。

### 从 v1.0.0 的 PostgreSQL 命名卷迁移

用旧 Compose 配置停掉 API。保持旧数据库运行，导出并检查备份。

```bash
docker compose stop api
docker compose exec -T db pg_dump -U gbwater -d gbwater -Fc > gbwater-v1.0.0.dump
docker compose exec -T db pg_restore --list < gbwater-v1.0.0.dump
```

确认导出成功后，用原配置执行 `docker compose down`，保留原命名卷。然后在原部署目录换成 v1.0.1 配置、按首次启动的命令创建挂载目录，并将备份恢复到新的空数据库：

```bash
docker compose up -d --wait db
docker compose exec -T db pg_restore -U gbwater -d gbwater --single-transaction --exit-on-error < gbwater-v1.0.0.dump
```

恢复成功后继续升级步骤。搜索索引会在后台发布时重建。升级验证通过前保留备份和原数据卷。

## 排查

排查时可用 `docker compose ps`、`docker compose logs api`、`curl http://127.0.0.1:8080/api/health` 和 `nginx -t`。Folia 与前端构建日志可在后台「发布」查看。
