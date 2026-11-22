import { createHash } from 'node:crypto'
import {
  copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, realpathSync, renameSync, rmdirSync, rmSync, writeFileSync,
} from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import lockfile from 'proper-lockfile'
import { build } from 'vite-ssg/node'
import { catalogCategories, parseCatalog, readCatalogSource, staticRoutes } from './static-content.ts'
import { prerenderedArticle } from '../src/lib/initial-article.ts'

const root = realpathSync.native(fileURLToPath(new URL('../', import.meta.url)))
process.chdir(root)
const dist = join(root, 'dist')
const publicDir = join(root, 'public')
const cacheFile = join(root, '.build-cache.json')

function hash(value) {
  return createHash('sha256').update(value).digest('hex')
}

function files(directory, exclude = new Set()) {
  if (!existsSync(directory)) return []
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    if (exclude.has(entry.name)) return []
    if (entry.isFile()) return [entry.name]
    if (entry.isDirectory()) {
      return files(join(directory, entry.name)).map(name => `${entry.name}/${name}`)
    }
    return []
  }).sort()
}

function fileHash(path) {
  try {
    return hash(readFileSync(path))
  } catch (error) {
    if (error.code === 'ENOENT') return undefined
    throw error
  }
}

function readCache() {
  try {
    return JSON.parse(readFileSync(cacheFile, 'utf8')).pages ?? {}
  } catch (error) {
    if (error.code === 'ENOENT' || error instanceof SyntaxError) return {}
    throw error
  }
}

function navigationByCourse(notes) {
  const courses = new Map()
  const courseKey = note => JSON.stringify([note.category, note.course])
  for (const note of notes) {
    const key = courseKey(note)
    if (!courses.has(key)) courses.set(key, [])
    courses.get(key).push([note.slug, note.title, note.order])
  }
  const fingerprints = new Map([...courses].map(([key, entries]) => [key, hash(JSON.stringify(entries))]))
  return note => fingerprints.get(courseKey(note))
}

async function buildPages(stage) {
  const catalogFile = join(publicDir, 'catalog.json')
  const catalogJSON = readCatalogSource(catalogFile)
  const catalog = parseCatalog(catalogJSON)
  const catalogHash = hash(catalogJSON)
  const categories = catalogCategories(catalog)
  const courseNavigationHash = navigationByCourse(catalog)
  const assets = new Map([['catalog.json', { contents: catalogJSON, digest: catalogHash }]])
  const articles = new Map(catalog.map(note => {
    const name = `note-content/${note.slug}.json`
    const filename = join(publicDir, name)
    if (!existsSync(filename)) throw new Error(`缺少正文：${note.id}`)
    const contents = readFileSync(filename, 'utf8')
    const digest = hash(contents)
    const body = JSON.parse(contents)
    if (typeof body.html !== 'string' || !/^[a-f0-9]{32}$/.test(body.revision)) {
      throw new Error(`正文产物格式不正确：${note.id}`)
    }
    assets.set(name, { contents, digest })
    const inputs = [note, digest, categories[note.category]?.name, courseNavigationHash(note)]
    return [`/notes/${note.slug}/`, { body, fingerprint: hash(JSON.stringify(inputs)) }]
  }))
  const previous = readCache()
  const pages = new Map()
  const reused = new Set()
  const rendered = new Set()

  await build({
    entry: '/scripts/ssg-entry.ts',
    includedRoutes(paths) {
      // 同时检查客户端资源和 SSR 使用的源码。目录数据单独判断，
      // 改一篇文章时，其它文章可以沿用相同的前端资源。
      const inputs = ['index.html', 'vite.config.ts', 'package-lock.json',
        ...files(join(root, 'scripts')).map(name => `scripts/${name}`),
        ...files(join(root, 'src'), new Set(['generated'])).map(name => `src/${name}`),
        'src/generated/site-settings.json']
      const shared = hash(JSON.stringify([
        process.version,
        fileHash(join(stage, 'index.html')),
        fileHash(join(stage, '.vite/ssr-manifest.json')),
        ...inputs.map(name => [name, fileHash(join(root, name))]),
      ]))
      return [...new Set(staticRoutes(paths, catalog, Object.keys(categories)))].filter(route => {
        const filename = join(route.slice(1), 'index.html').replaceAll('\\', '/')
        const dependsOnCatalog = route === '/' || route.startsWith('/library') || route.startsWith('/courses/')
        const input = hash(JSON.stringify([shared, articles.get(route)?.fingerprint ?? (dependsOnCatalog ? catalogHash : '')]))
        const cached = previous[filename]
        if (cached?.input === input && cached.output === fileHash(join(dist, filename))) {
          reused.add(filename)
        }
        pages.set(filename, { route, input })
        return !reused.has(filename)
      })
    },
    onBeforePageRender(route, _html, { app }) {
      const article = articles.get(route)
      if (article) app.provide(prerenderedArticle, article.body)
    },
    onPageRendered(route, html) {
      if (articles.has(route) && !html.includes('class="note-article"')) {
        throw new Error(`正文未写入静态页：${route}`)
      }
      rendered.add(route)
    },
  }, { root, build: { outDir: stage, copyPublicDir: false } })

  if ([...assets].some(([name, asset]) => fileHash(join(publicDir, name)) !== asset.digest)) {
    throw new Error('构建期间笔记产物发生变化，请等 Go 渲染结束后重试。')
  }
  // Vite 的客户端构建会写出 HTML 壳，首页沿用上次产物时应丢弃它。
  if (reused.has('index.html')) rmSync(join(stage, 'index.html'))
  rmSync(join(stage, '.vite/ssr-manifest.json'), { force: true })
  for (const [filename, page] of pages) {
    if (!reused.has(filename) && !rendered.has(page.route)) {
      throw new Error(`静态页面未生成：${page.route}`)
    }
    page.output = fileHash(join(reused.has(filename) ? dist : stage, filename))
    if (!page.output) throw new Error(`静态页面文件缺失：${page.route}`)
  }
  console.log(`[pages] rendered ${rendered.size}, reused ${reused.size}`)
  for (const [name, { contents, digest }] of assets) {
    if (digest === fileHash(join(dist, name))) {
      reused.add(name)
    } else {
      const filename = join(stage, name)
      mkdirSync(dirname(filename), { recursive: true })
      writeFileSync(filename, contents)
    }
  }
  return { pages, reused }
}

function publish(stage, { pages, reused }) {
  const staged = files(stage)
  const expected = new Set([...staged, ...reused])
  const publicFiles = files(publicDir).filter(name =>
    !expected.has(name) && !name.startsWith('note-content/'))
  publicFiles.forEach(name => expected.add(name))
  const obsolete = files(dist).filter(name => !expected.has(name))
  const updates = staged.filter(name => fileHash(join(stage, name)) !== fileHash(join(dist, name)))
  const publicUpdates = publicFiles.filter(name => fileHash(join(publicDir, name)) !== fileHash(join(dist, name)))
  // 增量复制静态资源，避免每次构建都复制所有图片和公式 SVG。
  for (const name of publicUpdates) {
    const target = join(stage, name)
    mkdirSync(dirname(target), { recursive: true })
    copyFileSync(join(publicDir, name), target)
  }
  // 新资源先落盘，随后替换引用这些资源的页面，最后清理旧产物。
  for (const name of [...publicUpdates, ...updates.filter(name => !pages.has(name)), ...updates.filter(name => pages.has(name))]) {
    const target = join(dist, name)
    mkdirSync(dirname(target), { recursive: true })
    renameSync(join(stage, name), target)
  }
  for (const name of obsolete) {
    const target = join(dist, name)
    rmSync(target)
    let directory = dirname(target)
    while (directory !== dist && readdirSync(directory).length === 0) {
      rmdirSync(directory)
      directory = dirname(directory)
    }
  }

  const manifest = join(stage, '.build-cache.json')
  writeFileSync(manifest, JSON.stringify({ pages: Object.fromEntries(pages) }) + '\n')
  renameSync(manifest, cacheFile)
  const updated = updates.length + publicUpdates.length
  console.log(`[publish] updated ${updated}, removed ${obsolete.length}, kept ${expected.size - updated}`)
}

const unlock = await lockfile.lock(root, { lockfilePath: join(root, '.build-lock'), stale: 30_000 })
try {
  const stage = mkdtempSync(join(root, '.dist-build-'))
  try {
    publish(stage, await buildPages(stage))
  } finally {
    rmSync(stage, { recursive: true, force: true })
  }
} finally {
  await unlock()
}
