# 架构

## 总览

站点是「静态页面 + Go API」：

- **前端编译**：开发机和镜像构建阶段用 Vite 编译 Vue，生成一份 HTML 模板和浏览器资源。
- **运行期**：Go 用编译模板安装登录和后台页面，调用 Folia 渲染内容并增量发布静态文件。nginx 托管文件，将公开 API 前缀代理到 Go 的 `/api/*`。

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
| `web/compiled/` | 开发阶段编译的 HTML 模板和浏览器资源 |
| `web/src/generated/` | Go 渲染缓存和发布清单 |
| `web/public/generated/` | 公式/Mermaid SVG（忽略提交） |

## 文章生成管线

1. Markdown 存在 `content/学科/课程/标题.md`。图床原图存入 `web/public/content-assets/`。
2. 「构建并发布」把任务写入 PostgreSQL 队列。Go 按入队顺序逐个执行，在任务开始时读取笔记版本、图片清单和站点设置。日常编辑、上传、回收和恢复通过后台 API 更新这些记录。已有数据在首次启动时登记一次。
   - 用 `folia_md` 解析 Markdown，从同一棵语法树生成正文和公式、Mermaid 占位；
   - Go 从占位读取公式和图表，调用 Folia 生成 SVG，再将占位替换为引用 SVG 的元素。图片替代文字与无障碍标签使用固定文案；
   - 行内公式使用 Folia 返回的断点，行间公式按阅读宽度选择预生成的排版，两种公式的 SVG 尺寸都以正文的 `em` 为单位。同一个公式的各段和各宽度排版复用同一个 SVG 文件；
   - 从渲染后 HTML 提取摘要、章节标题和锚点，略过公式、图片、图表和代码。
   - 输出 `web/public/catalog.json` 和 `web/public/note-content/<slug>.json`。
     正文 JSON 同时包含 HTML 和对应版本。渲染缓存保存在 `web/src/generated/`，未修改的内容复用 HTML、摘要、目录和正文版本。
3. Go 根据页面模板、目录数据和正文版本生成改动的页面。发布清单记录各文件的输入版本，只写入变化的产物并移除已撤下的页面或资源。站点设置和图标通过各自的静态文件更新。

「重建此篇」从编辑器使用单篇最新保存的 Markdown，重新生成正文、公式和图表。公开笔记会更新自己的页面和相关目录，其他笔记和站点设置沿用已发布版本。草稿重建用于编辑预览。首次发布站点或更改挂载路径后，需从发布页执行一次全站发布，建立单篇重建所需的公开目录。

发布页的「全站重建」重新渲染全部笔记。三种任务共用一个队列，重建完成后仍按产物版本写入变化的文件。

**Folia 的角色**：只做 Markdown 解析、数学/Mermaid 原生渲染；不使用它的
`folia::core.exporter`。HTML writer 在 `native/folia_bridge/bridge.cpp`，负责把 AST
映射成站点需要的 class（callout、content-center、minipage、表格、脚注、Highlight 等）。

### 为什么不在浏览器渲染 Markdown

- 内容不打包进客户端，降低首屏体积；
- 公式和图表在发布时生成并缓存，页面无需加载 KaTeX/Mermaid；
- 文章 HTML 直接写入公开页。Vue 挂载时复用这份正文并加载阅读交互。

内容更新通过后台「构建并发布」生成。Go 在任务子进程里加载原生库，构建超时可终止该子进程。

## 前端文章加载

- 首次打开笔记页：`main.ts` 在挂载前读取 Go 写入的 `.note-article` 和正文版本；
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
| `user_highlights` | 划线（颜色、quote、前后文、标题和正文版本） |
| `user_bookmarks` | 单篇阅读位置书签 |
| `user_sticky_notes` | 便条（锚点、内容和颜色） |
| `annotation_articles` | 各正文版本供匹配使用的文本 |
| `annotation_positions` | 各正文版本的半开整数区间；空值表示该锚点已计算且没有可靠位置 |
| `user_settings` | 阅读设置 JSONB |

锚点保存原始引文、前后文、标题和读者所见的正文版本。发布时，Go 从 Folia 渲染后的 HTML
提取文本，统一略过空白和图片，再对正文变化的笔记计算定位。匹配用完整引文对齐正文的一个
连续区间，插入、删除、替换每个字符各记一次编辑。编辑次数最多为引文长度的 25%，长度差
也受同一个预算约束。编辑数相同时比较前后文，仍有并列时表示未定位。

浏览器按当前正文版本读取位置，并把 Unicode 字符区间映射到 DOM。数据库保留原始锚点，
未定位的划线和便条仍可在标注面板查看，便条照常编辑。恢复原文后可再次定位。

读取某篇正文的标注时，划线、书签和便条从同一个数据库快照取得。便条和书签的局部更新
只写请求包含的字段。发布新的正文版本时清理已无对应标注的定位缓存，历史正文版本仍可读取。

账户标注按笔记 ID（`分类/课程/文件名`）保存。修改正文或网址 slug 时，标注继续随原 ID
读取。移动或重命名源文件会产生新的 ID，现有标注保存在原 ID 下。

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
