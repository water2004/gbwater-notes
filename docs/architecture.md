# 架构

## 总览

站点是「静态页面 + Go API」：

- **构建期**：Linux Go 服务调用 Folia 生成正文 HTML、公式、Mermaid SVG 和目录，然后由 Vite SSG 生成静态页面。
- **运行期**：nginx 托管静态页面，将公开 API 前缀代理到 Go 的 `/api/*`，包括账号、标注和搜索。

```
浏览器 ──静态页/预渲染 HTML── nginx
   │
   └── /mypdf/api/* ── Go（PostgreSQL + Meilisearch）
```

## 目录边界

| 路径 | 职责 |
|---|---|
| `content/` | 唯一公开内容源；frontmatter 与文件路径强校验 |
| `native/folia_bridge/` | 独立 C++23 可执行文件 `folia_md`，只依赖 `folia::markdown` |
| `internal/render/` | 构建期编排：native SVG、HTML 片段注入、catalog |
| `internal/app/` | HTTP 服务、内容写入、PostgreSQL、Meilisearch、认证、标注 |
| `web/` | Vue 3 页面与组件；文章正文不进入客户端 bundle |
| `web/src/generated/` | 后台构建生成的渲染缓存和配置快照 |
| `web/public/generated/` | 公式/Mermaid SVG（忽略提交） |

## 文章生成管线

1. Markdown 存在 `content/学科/课程/标题.md`。图床原图存入 `web/public/content-assets/`。
2. 后台「构建并发布」触发 Go 构建任务，扫描 `content/`：
   - 用 `folia_md` 把 Markdown 解析成 AST，直接写站点 HTML；
   - 同时用 Go 的 Folia native 封装渲染 `$…$`/`$$…$$` 和 Mermaid 为 SVG；
   - 再把 HTML 里的公式/Mermaid 占位替换成 `<img>`/`<figure>`；
   - 输出 `web/public/catalog.json` 和 `web/public/note-content/<slug>.json`。
     正文 JSON 同时包含 HTML 和对应版本。中间 HTML 与原生渲染元数据在 `web/src/generated/`。
3. `vite-ssg` 在构建时把每篇页面渲染成 HTML，正文来自第 2 步的片段；客户端只加载 Vue 壳。

**Folia 的角色**：只做 Markdown 解析、数学/Mermaid 原生渲染；不使用它的
`folia::core.exporter`。HTML writer 在 `native/folia_bridge/bridge.cpp`，负责把 AST
映射成站点需要的 class（callout、content-center、minipage、表格、脚注、Highlight 等）。

### 为什么不在浏览器渲染 Markdown

- 内容不打包进客户端，降低首屏体积；
- 公式/Mermaid 只渲染一次，页面无需加载 KaTeX/Mermaid；
- 文章 HTML 与站点样式在构建期确定，SSG 直接复用。

内容更新通过后台「构建并发布」生成。Go 在任务子进程里加载原生库，构建超时可终止该子进程。

## 前端文章加载

- 首次打开笔记页：`main.ts` 在挂载前把 SSR HTML 里的 `.note-article` 内容取走，避免
  Vue 挂载清空；
- 站内切换笔记请求 `note-content/<slug>.json`。开发服务器通过 Vite 的 `public/` 目录
  提供正文，生产托管发布目录里的同一份 JSON。

## 账号与会话

- 表：`users`、`user_sessions`、`email_codes`；密码 bcrypt；会话令牌只存 SHA-256。
- 角色：`admin` / `user`；数据库没有管理员时，第一个注册用户自动成为管理员。
- Cookie：HttpOnly、SameSite=Lax、生产环境 Secure；默认 30 天滑动过期。
- 认证限流和注册邮箱域名规则在后台管理并保存到 PostgreSQL。进程内按 IP 和账号限流，每个邮箱的验证码发送额度由 PostgreSQL 串行检查。
- SMTP 由 Go 服务环境配置，开发环境可用 `log` 模式查看验证码。
- 同源校验：按 Host 的 hostname 比较，忽略端口，兼容反代。

## 标注与阅读数据

| 表 | 内容 |
|---|---|
| `user_note_progress` | 每篇阅读进度、滚动比例 |
| `user_favorites` | 收藏 |
| `user_highlights` | 划线（颜色、quote、前后文、标题、revision、状态） |
| `user_bookmarks` | 单篇阅读位置书签 |
| `user_sticky_notes` | 便条（锚点、内容和颜色） |
| `user_memos` | 每篇私人笔记 |
| `user_settings` | 阅读设置 JSONB |

锚点保存原文引用、前后文、标题和保存时所见的正文版本。发布的正文自带版本号。
打开文章时，前端在当前正文里匹配引文，用前后文区分重复内容，并兼容格式化引入的空白。
定位结果只用于本次显示，数据库保留原始锚点。找不到原文时，划线和便条仍可在
标注面板查看，便条仍可编辑。恢复原文后的再次定位仍使用原始锚点。

账户标注按笔记 ID（`分类/课程/标题`）保存。修改正文、调整章节、变更网址 slug 或
暂时下架都保留同一 ID 的标注。移动或重命名源文件涉及笔记 ID 的迁移。

### 视觉层级

文章内有两类高亮，顺序固定：

1. Markdown 荧光笔（`==…==`）：渲染为透明 `<mark>`，背景/底线画在
   `.markdown-highlight-layer`（`z-index:-2`）；
2. 用户划线：`.highlight-layer` 的色带（`z-index:-1`）；
3. 正文文字。

这样用户划线不会被荧光笔底色盖住，文字始终最上层。

## 内容存储原则

- `content/*.md` 是正文唯一事实来源；
- PostgreSQL 只存元数据/账号/标注，不复制正文；
- Meilisearch 只存搜索索引，可从内容重建；
- 生成的正文 JSON、渲染缓存、SVG 和目录都可以通过 Go 渲染重新生成。

## 部署路径

设置 `GBWATER_BASE_PATH` 后在后台构建。该路径用于正文里的资源地址、前端路由、
静态托管和邮件链接。nginx 托管生成的 `web/dist` 并将 `GBWATER_API_BASE_PATH` 对应的路径转发给 Go。容器启动时安装登录和后台页面，公开静态页由管理员在后台发布。

## 已知边界

- Folia 原生渲染目前在 Windows 上执行。
- 后台编辑器预览仍是客户端 markdown-it，与公开文章的 Folia writer 是两套代码。
- 开发库不维护 schema 迁移。
