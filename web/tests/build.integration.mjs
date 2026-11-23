import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { spawnSync } from 'node:child_process'
import { cpSync, existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, renameSync, rmSync, statSync, symlinkSync, unlinkSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { basename, dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { deploymentPaths, isDynamicPath } from '../scripts/deployment.ts'
import { prepareFrontend } from '../scripts/prepare-frontend.mjs'

const repo = fileURLToPath(new URL('../../', import.meta.url))
const fixture = mkdtempSync(join(tmpdir(), 'gbwater-build-qa-'))
const web = join(fixture, 'web')
const publicDir = join(web, 'public')
const dist = join(web, 'dist')
const exclude = new Set(['node_modules', 'generated', 'dist', '.build-cache.json', '.vite-ssg-temp'])
mkdirSync(web)
for (const name of ['index.html', 'vite.config.ts', 'tsconfig.json', 'package.json', 'package-lock.json', 'src', 'scripts']) {
  cpSync(join(repo, 'web', name), join(web, name), {recursive:true, filter: path => !exclude.has(basename(path))})
}
mkdirSync(join(web, 'public'), {recursive:true})
cpSync(join(repo, 'web/public/favicon.svg'), join(web, 'public/favicon.svg'))
const imageDir = join(fixture, 'data', 'content-assets')
mkdirSync(imageDir, { recursive: true })
writeFileSync(join(imageDir, 'test.svg'), '<svg xmlns="http://www.w3.org/2000/svg" />')
writeFileSync(join(imageDir, 'unregistered.svg'), '<svg xmlns="http://www.w3.org/2000/svg" />')
symlinkSync(imageDir, join(publicDir, 'content-assets'), 'junction')
mkdirSync(join(fixture, 'internal/app'), {recursive:true})
cpSync(join(repo, 'internal/app/default_categories.json'), join(fixture, 'internal/app/default_categories.json'))
symlinkSync(join(repo, 'web/node_modules'), join(web, 'node_modules'), 'junction')
const makeNote = (category, course, slug, title, order) => ({
  id: `${category}/${course}/${title}`, category, categoryTitle: category === 'math' ? '数学' : '物理',
  course, courseSlug: course, slug, title, order, draft: false, excerpt: title, characters: 12, readingMinutes: 1, headings: [],
})
const first = makeNote('math', 'analysis', 'first', '甲篇', 1)
const sibling = makeNote('math', 'analysis', 'sibling', '乙篇', 2)
const unrelated = makeNote('physics', 'mechanics', 'unrelated', '丙篇', 1)
const newSubject = makeNote('astronomy', 'stars', 'new-star-note', '新的天文笔记', 1)
const revision = '1234567890abcdef'.repeat(2)
const catalog = join(publicDir, 'catalog.json')
const body = (note) => join(publicDir, 'note-content', note.slug + '.json')
const writeArticle = (note, html, articleRevision = revision) =>
  writeFileSync(body(note), JSON.stringify({ html, revision: articleRevision }))
for (const note of [first, sibling, unrelated]) {
  mkdirSync(dirname(body(note)), {recursive:true})
  writeArticle(note, `<p>${note.title}的正文</p>`)
}
writeArticle({ slug: 'unlisted' }, '<p>不在公开目录里</p>')
writeFileSync(catalog, JSON.stringify([first, sibling, unrelated]))
function build(expected = 0, env = {}) {
  const selected = JSON.parse(readFileSync(catalog, 'utf8'))
  const rendered = {}
  for (const name of ['catalog.json', ...selected.map(note => `note-content/${note.slug}.json`)]) {
    const file = join(publicDir, name)
    if (existsSync(file)) rendered[name] = createHash('sha256').update(readFileSync(file)).digest('hex')
  }
  const input = JSON.stringify({ rendered, assets: ['test.svg'] })
  const result = spawnSync(process.execPath, ['scripts/build.mjs'], {cwd: web, input, encoding:'utf8', env: {...process.env, GBWATER_BASE_PATH:'/regression/', ...env}})
  assert.equal(result.status, expected, result.stdout + result.stderr)
  const summary = result.stdout.split(/\r?\n/).filter(line => /\[(?:pages|publish)\]/.test(line)).join(' ')
  console.log(summary || result.stderr.split(/\r?\n/).find(line => line.startsWith('Error:')))
  return summary
}
function fileTimes(dir = dist) {
  return Object.fromEntries(readdirSync(dir, {withFileTypes:true}).flatMap(entry => {
    if (entry.name === '.build-cache.json') return []
    const filename = join(dir, entry.name)
    if (entry.isDirectory()) return Object.entries(fileTimes(filename)).map(([rel, time]) => [entry.name + '/' + rel, time])
    return [[entry.name, statSync(filename).mtimeMs]]
  }))
}
try {
  assert.match(build(), /rendered \d+, reused 0/)
  const initial = fileTimes()
  assert.match(readFileSync(join(dist, 'notes/first/index.html'),'utf8'), /lang="zh-CN"/)
  assert.match(readFileSync(join(dist, 'notes/first/index.html'),'utf8'), /\/regression\/library\/math/)
  assert.match(readFileSync(join(dist, 'notes/first/index.html'),'utf8'), new RegExp(`data-annotation-revision="${revision}"`))
  assert.deepEqual(readFileSync(join(dist, 'note-content/first.json')), readFileSync(body(first)))
  assert.deepEqual(readFileSync(join(dist, 'catalog.json')), readFileSync(catalog))
  assert.deepEqual(readFileSync(join(dist, 'content-assets/test.svg')), readFileSync(join(imageDir, 'test.svg')))
  assert(!existsSync(join(dist, 'content-assets/unregistered.svg')))
  assert(!existsSync(join(dist, 'note-content/unlisted.json')))
  assert.match(build(), /rendered 0, reused \d+.*updated 0, removed 0/)
  assert.deepEqual(fileTimes(), initial)
  writeArticle(first, '<p>甲篇正文改动</p>', 'a'.repeat(32))
  assert.match(build(), /rendered 1, reused \d+.*updated 2, removed 0/)
  const changed = fileTimes()
  assert.deepEqual(Object.keys(changed).filter(name => changed[name] !== initial[name]), ['note-content/first.json', 'notes/first/index.html'])
  assert.match(readFileSync(join(dist, 'notes/first/index.html'),'utf8'), /甲篇正文改动/)
  first.excerpt = '修改过的摘要'
  first.characters = 37
  writeFileSync(catalog, JSON.stringify([first, sibling, unrelated]))
  assert.match(build(), /rendered 3, reused \d+.*updated 4, removed 0/)
  const metadataUpdate = fileTimes()
  assert.equal(metadataUpdate['notes/sibling/index.html'], initial['notes/sibling/index.html'])
  assert.equal(metadataUpdate['notes/unrelated/index.html'], initial['notes/unrelated/index.html'])
  assert.notEqual(metadataUpdate['notes/first/index.html'], changed['notes/first/index.html'])
  assert.match(readFileSync(join(dist, 'courses/analysis/index.html'),'utf8'), /修改过的摘要/)
  assert.match(readFileSync(join(dist, 'index.html'),'utf8'), /修改过的摘要/)
  assert.equal(metadataUpdate['library/index.html'], initial['library/index.html'])
  assert.deepEqual(Object.keys(metadataUpdate).filter(name => name.startsWith('assets/')), Object.keys(initial).filter(name => name.startsWith('assets/')))
  mkdirSync(dirname(body(newSubject)), { recursive: true })
  writeArticle(newSubject, '<p>新的天文笔记的正文</p>')
  writeFileSync(catalog, JSON.stringify([newSubject, first, sibling, unrelated]))
  build()
  assert.match(readFileSync(join(dist, 'index.html'), 'utf8'), /新的天文笔记/)
  assert.match(readFileSync(join(dist, 'library/index.html'), 'utf8'), /\/regression\/library\/astronomy/)
  assert.match(readFileSync(join(dist, 'library/astronomy/index.html'), 'utf8'), /新的天文笔记/)
  writeFileSync(catalog, JSON.stringify([first, sibling]))
  build()
  assert(!existsSync(join(dist, 'notes/unrelated')))
  assert(!existsSync(join(dist, 'note-content/unrelated.json')))
  assert(!existsSync(join(dist, 'note-content/new-star-note.json')))
  assert(!existsSync(join(dist, 'courses/mechanics')))
  assert(!existsSync(join(dist, 'library/astronomy')))
  assert.doesNotMatch(readFileSync(join(dist, 'library/index.html'), 'utf8'), /\/regression\/library\/astronomy/)
  assert.doesNotMatch(readFileSync(join(dist, 'index.html'), 'utf8'), /新的天文笔记/)
  assert(!JSON.parse(readFileSync(join(dist,'catalog.json'),'utf8')).some(note => note.slug === 'unrelated'))
  const beforeFailure = fileTimes()
  rmSync(body(first))
  build(1)
  assert.deepEqual(fileTimes(), beforeFailure)

  writeArticle(first, '<p>恢复的正文</p>')
  const existing = join(fixture, 'published')
  const starter = join(fixture, 'starter')
  renameSync(dist, existing)
  build(0, { GBWATER_DYNAMIC_FRONTEND: 'template' })
  renameSync(dist, starter)
  assert(!existsSync(join(starter, 'index.html')))
  assert(!existsSync(join(starter, 'notes')))
  const fresh = join(fixture, 'fresh')
  prepareFrontend(starter, fresh, deploymentPaths({ GBWATER_BASE_PATH: '/regression/' }))
  assert.match(readFileSync(join(fresh, 'login/index.html'), 'utf8'), /\/regression\/favicon\.svg/)
  assert.deepEqual(readFileSync(join(fresh, 'favicon.svg')), readFileSync(join(publicDir, 'favicon.svg')))
  renameSync(existing, dist)
  const initialPaths = deploymentPaths({ GBWATER_BASE_PATH: '/regression/' })
  const movedPaths = deploymentPaths({ GBWATER_BASE_PATH: '/moved/', GBWATER_API_BASE_PATH: '/notes-api/' })
  prepareFrontend(starter, dist, initialPaths)
  assert.match(readFileSync(join(dist, 'login/index.html'), 'utf8'), /\/regression\/admin-assets\//)
  const afterStart = fileTimes()
  prepareFrontend(starter, dist, initialPaths)
  assert.deepEqual(fileTimes(), afterStart)
  prepareFrontend(starter, dist, movedPaths)
  assert.match(readFileSync(join(dist, 'admin/editor/index.html'), 'utf8'), /\/moved\/admin-assets\//)
  const afterMove = fileTimes()
  const publicTimes = times => Object.fromEntries(Object.entries(times).filter(([name]) => !isDynamicPath(name)))
  assert.deepEqual(publicTimes(afterMove), publicTimes(beforeFailure))
  const adminCode = readdirSync(join(dist, 'admin-assets'))
    .filter(name => name.endsWith('.js')).map(name => readFileSync(join(dist, 'admin-assets', name), 'utf8')).join('\n')
  assert.match(adminCode, /\/notes-api\//)
  assert.doesNotMatch(adminCode, /GBWATER_(?:API_)?BASE_PATH_PLACEHOLDER/)
  const installed = { GBWATER_DYNAMIC_FRONTEND: 'installed', GBWATER_BASE_PATH: '/moved/', GBWATER_API_BASE_PATH: '/notes-api/' }
  build(0, installed)
  assert.match(readFileSync(join(dist, 'notes/first/index.html'), 'utf8'), /\/moved\/library\/math/)
  const dynamicTimes = times => Object.fromEntries(Object.entries(times).filter(([name]) => isDynamicPath(name)))
  assert.deepEqual(dynamicTimes(fileTimes()), dynamicTimes(afterMove))
  const beforeIcon = fileTimes()
  const generatedIcon = join(web, 'src/generated/site-icon.svg')
  mkdirSync(dirname(generatedIcon), { recursive: true })
  const customIcon = '<svg xmlns="http://www.w3.org/2000/svg"><circle r="5"/></svg>'
  writeFileSync(generatedIcon, customIcon)
  assert.match(build(0, installed), /rendered 0, reused \d+.*updated 1, removed 0/)
  const afterIcon = fileTimes()
  assert.deepEqual(Object.keys(afterIcon).filter(name => afterIcon[name] !== beforeIcon[name]), ['favicon.svg'])
  prepareFrontend(starter, dist, movedPaths)
  assert.equal(readFileSync(join(dist, 'favicon.svg'), 'utf8'), customIcon)
  rmSync(generatedIcon)
  assert.match(build(0, installed), /rendered 0, reused \d+.*updated 1, removed 0/)
  assert.deepEqual(readFileSync(join(dist, 'favicon.svg')), readFileSync(join(publicDir, 'favicon.svg')))
  console.log('Incremental static build verified.')
} finally {
  // 已知路径由上面的 mkdtempSync 创建，先移除指向真实依赖目录的链接。
  unlinkSync(join(web, 'node_modules'))
  unlinkSync(join(publicDir, 'content-assets'))
  rmSync(fixture, { recursive: true, force: true })
}
