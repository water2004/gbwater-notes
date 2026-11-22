# 碧山秋云 · GBwater Notes

面向课程笔记的静态站和 Markdown 工作台。后台 Go 服务调用 Folia 生成正文、公式和 Mermaid SVG，Vite SSG 生成页面，nginx 托管静态文件并把 `/api/` 转发至 Go。

笔记支持分学科和课程浏览、搜索、划线和便签。管理员可直接编辑 Markdown、对比历史版本、管理单篇配图及发布站点。正文更新时只替换实际变化的发布文件。

## Linux 部署

部署方式沿用 Element Skin 的结构。Compose 运行 Go API、PostgreSQL 和 Meilisearch，宿主机 nginx 读取 `./frontend`。镜像包含 Linux Folia、Node 和初次安装用的空站前端。修改内容后在后台点击「构建并发布」。

本版本镜像为 `ghcr.io/water2004/gbwater-notes:1.0.0`。参见 [Linux 部署与运维](docs/deployment.md)。

## 本地开发

需要 Go 1.26、Node 24、PostgreSQL 和 Meilisearch。生成文章时还需安装 CMake 4.3.4、Ninja、Clang 21、libc++ 21、Pango 和 Fontconfig。镜像中的工具链配置可见 [Dockerfile](Dockerfile)。

```bash
git clone --recurse-submodules https://github.com/water2004/gbwater-notes.git
cd gbwater-notes
cp .env.example .env
mkdir -p content
npm ci --prefix web
npm ci --prefix third_party/folia/tools/mathjax-bundle --include=dev --ignore-scripts
node deploy/prepare-folia.mjs third_party/folia
CC=clang-21 CXX=clang++-21 cmake -S native/folia_bridge -B data/folia-bridge-build -G Ninja \
  -DCMAKE_BUILD_TYPE=Release '-DCMAKE_CXX_FLAGS=-stdlib=libc++ -fexperimental-library -pthread' \
  -DCMAKE_CXX_STDLIB_MODULES_JSON=/usr/lib/llvm-21/lib/libc++.modules.json \
  -DCMAKE_POSITION_INDEPENDENT_CODE=ON
cmake --build data/folia-bridge-build --target folia_md FoliaNativeMath FoliaNativeMermaid --parallel 3
go run ./cmd/gbwater
```

在 `.env` 填写本地数据库、搜索和管理员配置。在另一终端运行 `npm --prefix web run dev`，打开 [本地后台](http://127.0.0.1:5173/mypdf/admin) 并执行首次构建。笔记源文件放在 `content/学科/课程/标题.md`，图床原图由后台管理。

## 测试与发布

```bash
go vet ./...
go test ./...
go test -tags integration ./internal/app
npm --prefix web run typecheck
npm --prefix web test
npm --prefix web run test:build
```

GitHub Actions 在提交和 PR 中验证 Go、Vue、增量构建与 Linux 镜像。在 `main` 上推送 `vX.Y.Z` 标签后，测试通过的镜像发布到 GHCR，GitHub Release 记录镜像摘要。

业务数据存放在部署目录。发布仓库包含应用源码、部署示例和测试夹具。部署方自行维护笔记、图床与数据库。

项目源码采用 [MIT 协议](LICENSE)，第三方依赖沿用各自的许可。

- [架构与数据模型](docs/architecture.md)
- [Linux 部署与运维](docs/deployment.md)
- [HTTP API](docs/api.md)
- [版本说明](RELEASE.md)
