# 文档包

在后台「笔记管理」选择一篇或多篇笔记，点击「下载文档包」。在目标站点点击「导入文档包」，选中本地文件并检查预览。新增学科随包登记，目标站点已有的学科设置继续使用。导入完成后可在后台发布页面。

预览列出新增、已存在和将替换的笔记。相同完整笔记 ID 的新内容需要勾选确认替换，旧 Markdown 进入版本历史。同一个网址标识被其他笔记 ID 使用时，导入会提示冲突。提交时会重新检查目标内容，预览之后发生的改动需要重新确认。

同路径同内容的配图会复用。同路径但内容不同的图片会获得新路径，导入的笔记会引用新版。替换后不再需要的专属配图仅在没有笔记继续引用时移到回收站。恢复引用了这些图片的旧 Markdown 时，可以同时从回收站恢复图片。外部图片链接保留原地址。

## ZIP 格式

默认扩展名为 `.gbnotes.zip`，可以用普通 ZIP 工具打开，也可以用程序生成。包内文件按以下结构存放。

- `manifest.json` 提供 `format: "gbwater-notes"`、`version: 1`、学科映射 `categories`、笔记数组 `notes` 和图片数组 `assets`。
- `notes/<学科ID>/<课程>/<标题>.md` 使用 UTF-8，包含项目的 Markdown frontmatter。对应清单条目含 `id`、原始文件字节的十六进制 `sha256` 和 `metadata`。
- `metadata` 包含 `title`、`category`、`categoryTitle`、`course`、`courseSlug`、`slug`、`order` 和 `draft`。这些值须与 Markdown 中的元数据一致。
- `assets/<图片ID>` 存放引用的站内图片及所选笔记的专属图片。对应条目含 `id` 和 `sha256`。包内的站内图片使用 `/content-assets/<图片ID>`，路径段可用 URL 编码。
- `categories` 以学科 ID 为键，以包含 `name`、`description` 的对象为值。每个笔记使用的学科都要列出。

文档包保存当前笔记和配图。版本历史、用户账号、划线、便签和站点其他设置各自保存在原站点。

上传上限为 64 MiB，解压后上限为 128 MiB。单篇 Markdown 最大 8 MiB，单张图片最大 10 MiB。支持 PNG、JPEG、GIF、WebP 和仅依赖本地定义的静态 SVG。文件必须与清单声明和 SHA-256 一致。
