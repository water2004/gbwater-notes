# HTTP API

表中路径使用 Go 服务内部的 `/api/`。浏览器和外部客户端应使用部署时的 `GBWATER_API_BASE_PATH`，默认 `/mypdf/api/`，例如 `/mypdf/api/health`。图床上传使用图片的原始字节，其余带请求体的接口使用 `Content-Type: application/json` 并只发送一个 JSON 值。认证使用 HttpOnly 会话 Cookie。写请求携带 `Origin` 时校验主机名，忽略端口。

业务错误返回 HTTP 状态码和以下 JSON：

```json
{ "error": "错误消息", "code": "错误码" }
```

登录保护接口在匿名访问时返回 `401 unauthenticated`。普通读者访问后台接口时返回 `403 forbidden`。

## 认证

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/auth/email-code` | 提交 `email`、`purpose`（`register` 或 `reset`），返回 `{ok, cooldownSeconds}` |
| GET | `/api/auth/registration` | 返回注册是否开放和白名单域名 `{enabled, emailDomains}`，供注册页显示邮箱后缀 |
| POST | `/api/auth/register` | 提交 `username`、`email`、`password`、`code`。成功返回 `201` 和 `{user}`，同时登录 |
| POST | `/api/auth/login` | 提交 `identifier`（邮箱或用户名）、`password`，返回 `{user}` |
| POST | `/api/auth/logout` | 吊销当前会话 |
| GET | `/api/auth/session` | `{ "user": {...} }`，匿名 `{ "user": null }` |
| POST | `/api/auth/password/forgot` | 提交 `email`、`code`、`password`，重置后吊销全部会话 |
| POST | `/api/auth/password/change` | 登录后提交 `currentPassword`、`newPassword`，修改后吊销其他会话 |
| PATCH | `/api/auth/profile` | 登录后提交 `username`，返回 `{user}` |

认证默认在每个 15 分钟窗口内允许 5 次请求。登录分别按 IP 和账号计数，注册、发送验证码和提交密码重置各自按 IP 计数。超限返回 `429`，并带 `Retry-After` 秒数。

同一邮箱同一用途的验证码请求间隔至少 60 秒，注册和重置请求共用每邮箱每小时 5 次的额度。验证码有效期为 10 分钟，最多尝试 5 次。同样的投递响应和冷却规则也用于未注册邮箱的找回密码请求。

注册和申请注册码均执行邮箱域名规则。域名按完整名称匹配，大小写等价。域名被限制时返回 `400 email_domain_denied`。邮件服务无法投递时返回 `503 mail_unavailable`。

## 阅读数据

以下接口需要登录。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/me/reading` | 返回 `{progress, favorites}` |
| PUT | `/api/me/reading` | 提交 `noteId` 和 `completed`，或传 `read: true` 登记阅读时间 |
| DELETE | `/api/me/reading?noteId=…` | 清除单篇进度 |
| POST | `/api/me/reading/merge` | 提交 `{progress}`，条目包含 `noteId`、`completed`、`updatedAt` 和可选的 `lastReadAt` |
| PUT | `/api/me/favorites` | 提交 `{noteId}` 收藏 |
| DELETE | `/api/me/favorites?noteId=…` | 取消收藏 |
| POST | `/api/me/favorites/merge` | 提交 `{noteIds}` 合并本地收藏 |
| GET | `/api/me/settings` | 返回 `{reading}` |
| PUT | `/api/me/settings` | 提交 `{reading}`，其中 `reading` 为 JSON 对象 |

## 标注

以下接口只访问当前用户的标注。查询参数中的 `noteId` 是笔记 ID，路径中的 `id` 是标注记录 ID。
读取位置、新建标注和移动书签必须提供当前正文 `revision`。格式无效返回 `400 invalid_revision`，
未登记的版本返回 `409 revision_unavailable`。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/me/annotations?noteId=…&revision=…` | 返回 `{revision, highlights, bookmarks, stickyNotes}`，每条标注的 `location` 对应该版本 |
| GET | `/api/me/annotations/summary` | 返回三类数量和最近的划线、书签，每类至多 10 条，只包含账号页所需字段 |
| POST | `/api/me/highlights` | 提交锚点、`color` 和 `comment`，返回 `201` 和 `{highlight}` |
| DELETE | `/api/me/highlights/{id}` | 删除划线 |
| POST | `/api/me/bookmarks` | 提交锚点、`label` 和 `position`，返回 `201` 和 `{bookmark}` |
| PATCH | `/api/me/bookmarks/{id}` | 提交 `revision` 及要修改的书签名称、锚点或位置，返回 `{bookmark}` |
| DELETE | `/api/me/bookmarks/{id}` | 删除书签 |
| POST | `/api/me/sticky-notes` | 提交 `noteId`、`revision`、`body`、`color` 和可选锚点，返回 `201` 和 `{stickyNote}` |
| PATCH | `/api/me/sticky-notes/{id}` | 修改 `body` 或 `color`，返回 `{stickyNote}` |
| DELETE | `/api/me/sticky-notes/{id}` | 删除便条 |

划线/便条锚点字段：

```json
{
  "noteId": "math/线性代数/矩阵",
  "quote": "选中的原文",
  "prefix": "前 40 字",
  "suffix": "后 40 字",
  "heading": "所在标题",
  "revision": "文章 revision"
}
```

`revision` 来自读者实际看到的正文。`location` 用 `{start, end}` 表示渲染正文的归一化
Unicode 字符区间，起点包含、终点排除。找不到可靠位置时省略该字段，保留原始锚点。

## 搜索

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/search?q=…` | 返回 `{results}`，每位登录用户每分钟可请求 30 次 |

匿名返回 401；超限返回 429 并带 `Retry-After`。

## 后台

以下接口需要 `admin` 角色，使用与阅读端相同的会话。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/admin/overview` | 内容与用户计数、阅读统计、最近修改的 Markdown 文件 |
| GET | `/api/admin/site-settings` | 读取可编辑的站点设置 |
| PUT | `/api/admin/site-settings` | 保存并返回站点设置 JSON。公开页在构建时读入 |
| GET | `/api/admin/auth-policy` | 返回 `{policy, mailMode, mailTls?}` |
| PUT | `/api/admin/auth-policy` | 提交 `rateEnabled`、`rateAttempts`、`rateWindowMinutes`、`emailMode`、`emailDomains`。保存后立即使用新规则 |
| GET | `/api/admin/notes` | 返回 `{notes}`，包括草稿 |
| POST | `/api/admin/notes` | 提交 `{note, body}`，其中 `body` 为 Markdown 正文。成功返回 `201` 和 `{note, content}` |
| GET | `/api/admin/assets` | 返回 `{assets}`，传 `trash=true` 列出回收站，可用 `noteId` 将范围限定到该笔记的图片目录 |
| POST | `/api/admin/assets` | 查询参数传 `name` 和可选的 `noteId`，请求体为图片原始字节。支持 PNG、JPEG、GIF、WebP，单图最大 10 MiB。成功返回 `201` 和 `{asset}` |
| PATCH | `/api/admin/assets` | 提交 `{id, restore}` 移入回收站或恢复。当前 Markdown 仍引用图片时返回 `409 asset_in_use` |
| GET | `/api/admin/asset/content?id=…` | 返回图床图片原始内容，传 `trash=true` 预览回收站中的图片 |
| GET | `/api/admin/asset/references?id=…` | 返回 `{references}`，列出当前 Markdown 中引用该图片的笔记 ID 和标题 |
| GET | `/api/admin/note?id=…` | 返回 `{note, content}`，其中 `content` 是当前完整 Markdown，含 front matter |
| GET | `/api/admin/note/download?id=…` | 下载当前 Markdown 原文件 |
| POST | `/api/admin/document-packages/export` | 接收 `{ids}`，下载 ZIP 文档包 |
| POST | `/api/admin/document-packages?preview=true` | 上传 ZIP 原始字节并预览笔记、学科与图片变动 |
| POST | `/api/admin/document-packages?digest=…&replace=true` | 提交同一 ZIP，`digest` 取自预览。替换已有笔记需要显式传入 `replace=true` |
| PATCH | `/api/admin/note` | 提交 `{id, draft, expectedRevision}`，切换笔记公开状态，返回 `{note}` |
| PUT | `/api/admin/note` | 提交 `{id, content, expectedRevision}`。返回 `{note, content}`，版本冲突返回 `409 revision_conflict` |
| GET | `/api/admin/note/native?id=…` | 读取构建生成的公式和 Mermaid 元数据，缺失时返回 `404 native_not_found` |
| GET | `/api/admin/revisions?noteId=…` | 返回指定笔记最近 50 条修订，按最新记录优先排序。每条含 `id`、`noteId`、`revision`、`title`、`action`、`createdAt` |
| GET | `/api/admin/revisions/{id}?noteId=…` | 按修订记录 ID 和笔记 ID 读取历史快照，返回 `{content}`。记录不存在或归属于其他笔记时返回 `404 revision_not_found` |
| GET | `/api/admin/build` | 返回构建开关、当前任务范围、状态、日志、时间和等待队列 `queue` |
| GET | `/api/admin/build/history` | 返回 `{builds}`，包含最近 20 次发布记录 |
| POST | `/api/admin/build` | 提交 `{mode, noteId?}`，范围为 `incremental`（默认）、`note`（必填 `noteId`）或 `all`。返回 `202` 和该任务的 ID、范围及入队状态。构建关闭时返回 `409 build_disabled` |
| GET | `/api/admin/users` | 可传 `query`、`limit`、`offset`，返回 `{users, total, limit, offset}` |
| PATCH | `/api/admin/users/{id}` | 修改 `role` 或 `status`，返回 `{user}` |
| DELETE | `/api/admin/users/{id}` | 删除用户，返回 `{ok}` |
| POST | `/api/admin/users/{id}/password` | 可提交 `{password}`，省略时生成临时密码。返回 `{ok, password, generated}` |

创建笔记的 `note` 包含 `title`、`category`、`course`，可选 `slug`、`courseSlug`、`order`、`draft`。更新时应将上次读取的 `note.revision` 作为 `expectedRevision`。历史快照可载入编辑器，保存时仍以当前文件版本做冲突检查。

首次编辑已有的 Markdown 文件时会补存编辑前的快照，该记录的 `action` 为 `baseline`。重复保存同一内容不增加快照。图床有单独的回收站。

`emailMode` 支持 `disabled`、`allowlist`、`denylist`，`emailDomains` 是邮箱域名数组。启用白名单时至少提供一个域名。

## 其他

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/health` | 存活检查 |
| POST | `/api/public/note-view` | 提交 `{id}` 记录公开笔记浏览量，成功返回 `202` 和 `{ok}` |
| GET | `/api/public/popular-notes` | 返回 `{notes}`，可传 `limit`，默认 6、最多 20 |
