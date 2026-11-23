# GBwater Notes v1.0.4 Linux 部署

使用已发布的 `ghcr.io/water2004/gbwater-notes:1.0.4` 镜像，平台为 `linux/amd64`。宿主机 nginx 托管 `./frontend/`，Compose 运行 Go API、PostgreSQL 和 Meilisearch。 [GitHub Release 与镜像摘要](https://github.com/water2004/gbwater-notes/releases/tag/v1.0.4)

下文命令都在部署目录执行。持久化数据使用 Compose 文件同级的目录，服务之间由 Compose 自动组网。 [Compose 默认网络](https://docs.docker.com/compose/how-tos/networking/)

## 首次启动

在 Linux 主机安装 Docker Engine、Compose 插件和 nginx。进入打算存放站点和数据的目录，下载 v1.0.4 的部署文件。

```bash
gbwater_deploy_src=https://raw.githubusercontent.com/water2004/gbwater-notes/v1.0.4
mkdir -p deploy
curl -fsSLo docker-compose.yml "$gbwater_deploy_src/docker-compose.yml"
curl -fsSLo deploy/compose.env.example "$gbwater_deploy_src/deploy/compose.env.example"
curl -fsSLo deploy/nginx.conf.example "$gbwater_deploy_src/deploy/nginx.conf.example"
install -m 0600 deploy/compose.env.example .env
```

在 `.env` 设置 `GBWATER_SITE_URL` 为实际的 HTTPS 域名，例如 `https://notes.example.com`。同时填写管理员邮箱和独立密码。数据库密码和搜索密钥各执行一次 `openssl rand -hex 24` 生成。Go 服务会在没有可用管理员时使用 `GBWATER_ADMIN_*` 创建管理员，已有管理员继续使用原账号和密码。

```bash
mkdir -p data/db data/meilisearch
sudo install -d -m 0755 -o 10001 -g 10001 content frontend data/gbwater data/gbwater/content-assets data/render
docker compose config --quiet
docker compose pull
docker compose up -d --wait db search
docker inspect -f '{{range .NetworkSettings.Networks}}{{.Gateway}}{{end}}' "$(docker compose ps -q db)"
```

把查到的网关填入 `.env` 的 `GBWATER_DOCKER_GATEWAY`。这个值供 API 信任宿主机 nginx 转发的真实访客 IP，Docker 继续自动分配网络。以后重建 Compose 网络时重新查一次。

```bash
docker compose up -d --wait
```

API 容器启动时由 Go 按 `.env` 的路径把登录、注册、找回密码和后台页面安装到 `frontend/`。公开首页、课程页和笔记由管理员首次发布生成。镜像构建阶段已将 Vue 编译为模板和浏览器资源，最终镜像和宿主机的运行路径均无需 Node。

## 配置宿主机 nginx

将下载的 nginx 示例合并到宿主机站点配置中。把域名、证书、示例中的 `/var/www/gbwater/frontend/` 和代理端口换成实际值。可以在部署目录运行 `realpath frontend` 确认静态目录，nginx 进程需要能进入部署目录并读取产物。 [nginx 完整示例](../deploy/nginx.conf.example)

| 用途 | 默认外部地址 | nginx 目标 |
| --- | --- | --- |
| 页面、前端资源、图片和公式 | `/mypdf/` | 部署目录下的 `frontend/` |
| 公开 API | `/mypdf/api/` | `http://127.0.0.1:8080/api/` |
| 文档包上传 | `/mypdf/api/admin/document-packages` | `http://127.0.0.1:8080/api/admin/document-packages` |

普通 API 的 `proxy_pass` 保留末尾的 `/api/`，nginx 用它替换匹配到的外部前缀。文档包使用独立的精确匹配 location，设置 `client_max_body_size 64m`。 [nginx 路径转发规则](https://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_pass)

更改挂载路径时，调整 `GBWATER_BASE_PATH` 以及 nginx 中的静态 location、编辑器重写和重定向。留空 `GBWATER_API_BASE_PATH` 时，公开 API 前缀取站点路径加 `api/`。例如挂载到 `/notes/` 时，默认 API 是 `/notes/api/`。也可以将 API 单独设为 `/notes-api/`，此时文档包的精确 location 是 `/notes-api/admin/document-packages`。代理目标保持表中的内部路由，端口跟随 `GBWATER_PORT`。

```bash
sudo nginx -t && sudo systemctl reload nginx
curl -fsS https://notes.example.com/mypdf/api/health
```

健康检查通过后，从 HTTPS 的 `/mypdf/admin/` 进入后台，再点击「构建并发布」生成公开首页和笔记。

## 修改和发布内容

1. 站点运行后，在后台编辑笔记和管理图片。需要批量导入时，从「笔记管理」上传 `.gbnotes.zip`，检查预览后确认新增或替换。 [文档包使用与格式](document-package.md)
2. 在「站点设置 → 站点外观 → 浏览器图标」上传并预览图标。保存设置后，图标会在下次发布时应用到全站。
3. 在「发布」点击「构建并发布」。任务进入 PostgreSQL 队列，按提交顺序执行。Go 根据保存的笔记版本、图片清单和站点设置增量更新 `frontend/`。
4. 等待后台显示「发布成功」，打开首页、课程目录和这次更新的笔记确认。

改路径时调整 `.env` 的 `GBWATER_BASE_PATH` 和 `GBWATER_API_BASE_PATH`、同步修改 nginx 的 location，再运行 `docker compose up -d --wait`。同一镜像即可启动新的登录和后台路径。公开页由管理员随后点击「构建并发布」，服务按新路径更新对应的静态产物。

## 更新正在运行的站点

在原部署目录将 `.env` 的 `GBWATER_IMAGE` 改为 `ghcr.io/water2004/gbwater-notes:1.0.4`。现有 Compose 继续使用原目录来挂载笔记、图床、数据库、搜索索引和静态文件。

```bash
docker compose pull api
docker compose up -d --wait --no-deps api
```

API 就绪后，在后台点击「构建并发布」。

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

## 文件与数据目录

| 路径 | 内容 |
| --- | --- |
| `content/` | Markdown 原文 |
| `data/gbwater/content-assets/` | 图床原图 |
| `data/gbwater/asset-trash/` | 图片回收站 |
| `data/render/` | 可重建的后台渲染数据 |
| `frontend/` | nginx 读取的静态发布目录 |
| `data/db/` | PostgreSQL 数据，包含账号、会话、划线便签、站点设置、Markdown 修订、内容清单和发布队列 |
| `data/meilisearch/` | 可重建的搜索索引 |

## 排查

先运行 `docker compose ps` 和 `docker compose logs api`，再分别检查 `http://127.0.0.1:8080/api/health` 与部署域名下的公开健康检查。Folia 和静态构建日志在后台「发布」查看。文档包上传收到 HTTP 413 时，核对实际生效的 nginx 精确 location 和 64 MiB 上限。
