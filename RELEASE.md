# GBwater Notes 1.0.3

- 生产镜像只携带 Go、Folia 和编译好的前端模板与静态资源。Go 安装登录和后台页面，并完成公开站点发布。
- 开发和生产共用 Go 发布器。发布根据文件版本增量写入，先准备改动的产物，再替换公开文件。
- 保留笔记、配图、图标和标注版本的发布流程，以及已有站点路径和 API 前缀配置。

镜像为 `ghcr.io/water2004/gbwater-notes:1.0.3`，平台为 Linux amd64。已有部署可更新 `GBWATER_IMAGE`、重启 API，再从后台构建并发布。 [Linux 部署指南](https://github.com/water2004/gbwater-notes/blob/v1.0.3/docs/deployment.md)
