# GBwater Notes 1.0.1

- 更新文章时，后端根据渲染后的正文重新定位划线、书签和便签。可以匹配正文中的插入、删除和改词。低于置信阈值的标注保留原始引文和内容，供后续发布重新定位。
- 统一文章标注 API 和数据库中的正文版本、定位结果。账户页移除了独立私人笔记功能。
- 容器启动时按配置安装登录和后台页面。公开页面由管理员增量发布。宿主机 nginx 托管静态站点和图片，并转发可配置的 API 前缀。
- Compose 使用默认网络。数据库、搜索索引和站点文件通过部署目录下的路径持久化。

镜像为 `ghcr.io/water2004/gbwater-notes:1.0.1`，目标平台 `linux/amd64`。升级时更新 Compose 和 nginx 配置，再到后台发布一次，计算已有标注的位置。原先使用 PostgreSQL 命名卷的部署需要先迁移数据库。 [Linux 部署与运维](https://github.com/water2004/gbwater-notes/blob/v1.0.1/docs/deployment.md)
