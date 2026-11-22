import { readFileSync } from 'node:fs'
import { initializeCatalog } from '../src/lib/content'
import type { NoteMeta } from '../src/types/content'

// 构建脚本从 web/ 运行。目录已由 Go 生成，预渲染直接使用同一份数据。
initializeCatalog(JSON.parse(readFileSync('public/catalog.json', 'utf8')) as NoteMeta[])

export { createApp } from '../src/main'
