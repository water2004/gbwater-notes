# GBwater Notes 1.0.0

- 按学科与课程浏览笔记，支持搜索、阅读划线与便签。
- 全宽 Markdown 工作台提供分栏预览、大纲、高亮、补全、版本对比和逐篇图片管理。
- 后台统一管理学科及公开文案，支持认证限流、注册邮箱域名和 SMTP。
- Linux 上由 Go 调用 Folia 渲染 Markdown、公式及 Mermaid，再生成静态页面。重复构建只更新改动的文件。

镜像为 `ghcr.io/water2004/gbwater-notes:1.0.0`，目标平台 `linux/amd64`。Compose 运行应用和依赖，宿主机 nginx 托管静态产物。[Linux 部署与运维](docs/deployment.md)
