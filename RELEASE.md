# GBwater Notes 1.0.0

- 按学科与课程浏览笔记，支持搜索、阅读划线与便签。
- 全宽 Markdown 工作台提供分栏预览、大纲、高亮、补全、版本对比和逐篇图片管理。
- 后台统一管理学科及公开文案，支持认证限流、注册邮箱域名和 SMTP。
- Linux 上由 Go 调用 Folia 渲染 Markdown、公式及 Mermaid，再生成静态页面。重复构建只更新改动的文件。

镜像为 `ghcr.io/water2004/gbwater-notes:1.0.0`，目标平台 `linux/amd64`。请使用当前部署配置，数据库和搜索索引保存在 Compose 文件同级的 `data/` 下，宿主机 nginx 托管静态产物。 [Linux 部署与运维](https://github.com/water2004/gbwater-notes/blob/main/docs/deployment.md)
