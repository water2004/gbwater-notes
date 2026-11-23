# GBwater Notes 1.0.1

- 后台支持批量下载和导入文档包，包含 Markdown、笔记和学科元数据、配图。替换笔记前提供预览与确认，旧正文进入版本历史，配图按引用复用、分配新路径或移入回收站。
- 发布任务通过 PostgreSQL 队列顺序执行，服务重启后继续处理未完成的任务。发布使用后台 API 保存的版本和图片清单，只更新实际发生变化的产物。
- 公式和 Mermaid 以 SVG 显示，摘要和目录从渲染结果生成。已移除站内打印功能。
- 更新文章时，后端根据渲染后的正文重新定位划线、书签和便签。可以匹配正文中的插入、删除和改词。低于置信阈值的标注保留原始引文和内容，供后续发布重新定位。
- 统一文章标注 API 和数据库中的正文版本、定位结果，移除独立私人笔记功能。
- 容器启动时按配置安装登录和后台页面。公开页面由管理员增量发布。宿主机 nginx 托管静态站点和图片，并转发可配置的 API 前缀。
- Compose 使用默认网络。数据库、搜索索引和站点文件通过部署目录下的路径持久化。

镜像为 `ghcr.io/water2004/gbwater-notes:1.0.1`，目标平台 `linux/amd64`。首次安装时，启动 Compose 并配置宿主机 nginx，通过后台导入内容后发布站点。 [Linux 部署指南](https://github.com/water2004/gbwater-notes/blob/main/docs/deployment.md) · [文档包](https://github.com/water2004/gbwater-notes/blob/v1.0.1/docs/document-package.md)
