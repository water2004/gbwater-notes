# GBwater Notes 1.0.2

- 修复 Linux 发布过程中 Folia 公式渲染偶发返回状态 1 的问题。创建、使用和释放公式渲染器统一在同一个系统线程执行。
- 后台「站点设置 → 站点外观」支持上传、预览和恢复浏览器图标。全站使用同一份图标，单独换图标只更新图标文件。
- 启动时为登录和后台页面安装默认图标，已发布的自定义图标会保留。
- 发布镜像前验证 Linux 公式和 Mermaid 渲染、重复构建及图标的增量发布。

Linux amd64 镜像为 `ghcr.io/water2004/gbwater-notes:1.0.2`。已有部署可在原目录更新 `GBWATER_IMAGE`，更新 API 后从后台重新发布站点。 [Linux 部署指南](https://github.com/water2004/gbwater-notes/blob/v1.0.2/docs/deployment.md)
