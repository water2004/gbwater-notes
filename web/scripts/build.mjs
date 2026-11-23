import { createHash } from 'node:crypto'
import {
  copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, realpathSync, renameSync, rmdirSync, rmSync, writeFileSync,
} from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import lockfile from 'proper-lockfile'
import { loadEnv } from 'vite'
import { build } from 'vite-ssg/node'
import { catalogCategories, catalogPageInputs, parseCatalog, readCatalogSource, staticRoutes } from './static-content.ts'
import { dynamicEntries, isDynamicPath } from './deployment.ts'
import { prerenderedArticle } from '../src/lib/initial-article.ts'

const root = realpathSync.native(fileURLToPath(new URL('../', import.meta.url)))
process.chdir(root)
const dist = join(root, 'dist')
const publicDir = join(root, 'public')
const cacheName = '.build-cache.json'
const stagePrefix = '.dist-build-'
const cacheFile = join(dist, cacheName)
const dynamicMode = process.env.GBWATER_DYNAMIC_FRONTEND
const customIcon = join(root, 'src', 'generated', 'site-icon.svg')
const preserve = dynamicMode === 'installed' ? [...dynamicEntries] : []
const publication = dynamicMode === 'template'
  ? { rendered: { 'catalog.json': fileHash(join(publicDir, 'catalog.json')) }, assets: [] }
  : JSON.parse(readFileSync(0, 'utf8'))

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

function publicSource(name) {
  return name === 'favicon.svg' && dynamicMode !== 'template' && existsSync(customIcon)
    ? customIcon
    : join(publicDir, name)
}

function readCache() {
  try {
    return JSON.parse(readFileSync(cacheFile, 'utf8'))
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
  const catalog = dynamicMode === 'template' ? [] : parseCatalog(catalogJSON)
  const categories = catalogCategories(catalog)
  const catalogInputs = catalogPageInputs(catalog, categories)
  const courseNavigationHash = navigationByCourse(catalog)
  const publicFiles = new Map(Object.entries(publication.rendered))
  publicFiles.set('favicon.svg', fileHash(publicSource('favicon.svg')))
  for (const id of publication.assets) {
    const name = `content-assets/${id}`
    publicFiles.set(name, id)
  }
  const articles = new Map(catalog.map(note => {
    const name = `note-content/${note.slug}.json`
    const filename = join(publicDir, name)
    const digest = publicFiles.get(name)
    if (!digest || !existsSync(filename)) throw new Error(`缺少正文：${note.id}`)
    const inputs = [note, digest, categories[note.category]?.name, courseNavigationHash(note)]
    return [`/notes/${note.slug}/`, { filename, id: note.id, fingerprint: hash(JSON.stringify(inputs)) }]
  }))
  const previous = readCache()
  const previousPages = previous.pages ?? {}
  const inputs = ['index.html', 'vite.config.ts', 'package.json', 'package-lock.json',
    '../internal/app/default_categories.json',
    ...files(join(root, 'scripts')).map(name => `scripts/${name}`),
    ...files(join(root, 'src'), new Set(['generated'])).map(name => `src/${name}`),
    'src/generated/site-settings.json']
  const frontend = hash(JSON.stringify([
    process.version, process.env.NODE_ENV, dynamicMode,
    loadEnv('production', dirname(root), ['GBWATER_BASE_PATH', 'GBWATER_API_BASE_PATH']),
    loadEnv('production', root, ['VITE_']),
    ...inputs.map(name => [name, fileHash(join(root, name))]),
  ]))
  const publicationKey = hash(JSON.stringify([frontend, publication.rendered['catalog.json'],
    [...articles].map(([route, article]) => [route, article.fingerprint])]))
  if (previous.publicationKey === publicationKey
    && Array.isArray(previous.bundle)
    && Object.keys(previousPages).every(name => existsSync(join(dist, name)))
    && previous.bundle.every(name => existsSync(join(dist, name)))) {
    const pages = new Map(Object.entries(previousPages))
    console.log(`[pages] rendered 0, reused ${pages.size}`)
    return { pages, reused: new Set(pages.keys()), bundle: previous.bundle, publicFiles, previous, publicationKey }
  }
  const pages = new Map()
  const reused = new Set()
  const rendered = new Set()

  await build({
    entry: '/scripts/ssg-entry.ts',
    includedRoutes(paths) {
      const shared = hash(JSON.stringify([
        frontend,
        fileHash(join(stage, 'index.html')),
        fileHash(join(stage, '.vite/ssr-manifest.json')),
      ]))
      return [...new Set(staticRoutes(paths, catalog, Object.keys(categories)))].filter(route => {
        if (dynamicMode === 'template' && !isDynamicPath(route)) return false
        if (dynamicMode === 'installed' && isDynamicPath(route)) return false
        const filename = join(route.slice(1), 'index.html').replaceAll('\\', '/')
        const input = hash(JSON.stringify([shared, articles.get(route)?.fingerprint ?? catalogInputs.get(route)]))
        const cached = previousPages[filename]
        if (cached?.input === input && existsSync(join(dist, filename))) {
          reused.add(filename)
        }
        pages.set(filename, { route, input })
        return !reused.has(filename)
      })
    },
    onBeforePageRender(route, _html, { app }) {
      const article = articles.get(route)
      if (article) {
        const body = JSON.parse(readFileSync(article.filename, 'utf8'))
        if (typeof body.html !== 'string' || !/^[a-f0-9]{32}$/.test(body.revision)) {
          throw new Error(`正文产物格式不正确：${article.id}`)
        }
        app.provide(prerenderedArticle, body)
      }
    },
    onPageRendered(route, html) {
      if (articles.has(route) && !html.includes('class="note-article"')) {
        throw new Error(`正文未写入静态页：${route}`)
      }
      rendered.add(route)
    },
  }, { root, build: { outDir: stage, copyPublicDir: false } })

  // Vite 的客户端构建会写出 HTML 壳，首页沿用上次产物时应丢弃它。
  if (!pages.has('index.html') || reused.has('index.html')) rmSync(join(stage, 'index.html'), { force: true })
  rmSync(join(stage, '.vite/ssr-manifest.json'), { force: true })
  for (const [filename, page] of pages) {
    if (!reused.has(filename) && !rendered.has(page.route)) {
      throw new Error(`静态页面未生成：${page.route}`)
    }
    page.output = reused.has(filename) ? previousPages[filename].output : fileHash(join(stage, filename))
    if (!page.output) throw new Error(`静态页面文件缺失：${page.route}`)
  }
  console.log(`[pages] rendered ${rendered.size}, reused ${reused.size}`)
  const bundle = files(stage).filter(name => !pages.has(name))
  return { pages, reused, bundle, publicFiles, previous, publicationKey }
}

function publish(stage, { pages, reused, bundle, publicFiles, previous, publicationKey }) {
  const staged = files(stage)
  const outputs = new Map([...staged.map(name => [name, pages.get(name)?.output ?? fileHash(join(stage, name))]),
    ...[...reused].map(name => [name, pages.get(name).output]), ...publicFiles])
  for (const name of bundle) {
    if (!outputs.has(name)) outputs.set(name, previous.outputs[name])
  }
  const expected = new Set(outputs.keys())
  const obsolete = (previous.outputs ? Object.keys(previous.outputs) : files(dist,
    new Set([cacheName, ...readdirSync(dist).filter(name => name.startsWith(stagePrefix)), ...preserve])))
    .filter(name => !expected.has(name) && !preserve.includes(name.split('/')[0]) && existsSync(join(dist, name)))
  const needsUpdate = name => previous.outputs?.[name] !== outputs.get(name) || !existsSync(join(dist, name))
  const updates = staged.filter(needsUpdate)
  const publicUpdates = [...publicFiles.keys()].filter(needsUpdate)
  // 增量复制静态资源，避免每次构建都复制所有图片和公式 SVG。
  for (const name of publicUpdates) {
    const target = join(stage, name)
    mkdirSync(dirname(target), { recursive: true })
    copyFileSync(publicSource(name), target)
  }
  if (updates.length || publicUpdates.length || obsolete.length) rmSync(cacheFile, { force: true })
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

  const metadata = JSON.stringify({
    publicationKey, bundle, pages: Object.fromEntries(pages),
    outputs: Object.fromEntries([...outputs].sort(([a], [b]) => a.localeCompare(b))),
  }) + '\n'
  if (hash(metadata) !== fileHash(cacheFile)) {
    const manifest = join(stage, cacheName)
    writeFileSync(manifest, metadata)
    renameSync(manifest, cacheFile)
  }
  const updated = updates.length + publicUpdates.length
  console.log(`[publish] updated ${updated}, removed ${obsolete.length}, kept ${expected.size - updated}`)
}

const unlock = await lockfile.lock(root, { lockfilePath: join(root, '.build-lock'), stale: 30_000 })
try {
  // Temporary files and the target share a filesystem even when dist is a bind mount.
  mkdirSync(dist, { recursive: true })
  const stage = mkdtempSync(join(dist, stagePrefix))
  try {
    publish(stage, await buildPages(stage))
  } finally {
    rmSync(stage, { recursive: true, force: true })
  }
} finally {
  await unlock()
}
