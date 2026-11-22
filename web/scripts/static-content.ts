import { existsSync, readFileSync } from 'node:fs'
import { readCategories, withCatalogCategories } from '../src/lib/categories.ts'
import type { NoteMeta } from '../src/types/content.ts'

const settingsFile = new URL('../src/generated/site-settings.json', import.meta.url)

export function readCatalogSource(catalogFile: string) {
  if (!existsSync(catalogFile)) {
    throw new Error('缺少公开笔记目录。')
  }
  return readFileSync(catalogFile, 'utf8')
}

export function parseCatalog(source: string): NoteMeta[] {
  const catalog = JSON.parse(source) as NoteMeta[]
  if (catalog.some(note => note.draft)) throw new Error('公开笔记目录包含草稿，请重新运行 Go 渲染。')
  return catalog
}

export function catalogCategories(notes: NoteMeta[]) {
  const settings = existsSync(settingsFile) ? JSON.parse(readFileSync(settingsFile, 'utf8')) : {}
  return withCatalogCategories(readCategories(settings.categories), notes)
}

export function staticRoutes(paths: readonly string[], notes: NoteMeta[], categoryIDs: string[]) {
  return [
    ...paths.filter(path => !path.includes(':') && !path.includes('*')),
    '/library',
    '/admin/editor',
    ...categoryIDs.map(id => `/library/${id}/`),
    ...new Set(notes.map(note => `/courses/${note.courseSlug}/`)),
    ...notes.map(note => `/notes/${note.slug}/`),
  ]
}
