# 碧山秋云 · GBwater Notes

面向课程笔记的静态站和 Markdown 工作台。Vue 在开发和镜像构建阶段编译。Go 调用 Folia 生成正文、公式和 Mermaid SVG，再使用编译模板写入静态页面。nginx 托管这些文件并转发配置的 API 路径。

笔记支持分学科和课程浏览、搜索、划线和便签。管理员可直接编辑 Markdown、对比历史版本、管理单篇配图及发布站点。正文更新时只替换实际变化的发布文件。

## Linux 部署

部署方式沿用 Element Skin 的结构。Compose 运行 Go API、PostgreSQL 和 Meilisearch，宿主机 nginx 读取 `./frontend`。新版镜像在启动时按部署路径安装登录和管理后台。公开页在后台点击「构建并发布」后增量更新。

Linux amd64 镜像为 `ghcr.io/water2004/gbwater-notes:1.0.4`。 [部署指南](docs/deployment.md) · [GitHub Release](https://github.com/water2004/gbwater-notes/releases/tag/v1.0.4)

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
npm --prefix web run build
go run ./cmd/gbwater
```

在 `.env` 填写本地数据库、搜索和管理员配置。在另一终端运行 `npm --prefix web run dev`，打开 [本地后台](http://127.0.0.1:5173/mypdf/admin) 并执行首次构建。开发和生产使用同一个 Go 发布器，发布产物写到 `web/dist/`。Vite 开发服务器用于 Vue 热更新。笔记源文件放在 `content/学科/课程/标题.md`，图床原图由后台管理。

## 测试与发布

```bash
go vet ./...
go test ./...
go test -tags integration ./internal/app
npm --prefix web run typecheck
npm --prefix web test
npm --prefix web run build
```

在 GitHub 发布指向 `main` 提交的正式 `vX.Y.Z` Release 后，Actions 才运行测试和镜像构建，通过后发布到 GHCR，并把镜像摘要记录在 Release。平时可用上面的命令在本地检查。

业务数据存放在部署目录。发布仓库包含应用源码、部署示例和测试夹具。部署方自行维护笔记、图床与数据库。

项目源码采用 [MIT 协议](LICENSE)，第三方依赖沿用各自的许可。

- [架构与数据模型](docs/architecture.md)
- [Linux 部署与运维](docs/deployment.md)
- [HTTP API](docs/api.md)
- [文档包使用与格式](docs/document-package.md)
- [版本说明](RELEASE.md)
