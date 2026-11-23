import { existsSync, readFileSync } from 'node:fs'
import { readCategories, withCatalogCategories, type Categories } from '../src/lib/categories.ts'
import { groupCourses } from '../src/lib/course-groups.ts'
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

export function catalogPageInputs(notes: NoteMeta[], categories: Categories) {
  const groups = groupCourses(notes)
  const subjects = Object.entries(categories).map(([id, category]) => ({
    id, name: category.name, courses: groups.filter(group => group.category === id),
  }))
  const tabs = subjects.map(subject => [subject.id, subject.name, subject.courses.length])
  const pages = new Map<string, unknown>()
  const featured = notes[0]
  if (featured) {
    pages.set('/', [featured.id, featured.slug, featured.title, featured.course, featured.excerpt,
      featured.readingMinutes, categories[featured.category]?.name ?? featured.categoryTitle])
  }
  for (const subject of subjects) {
    const cards = subject.courses.map(group => [group.course, group.courseSlug,
      group.notes.map(note => [note.slug, note.title])])
    pages.set(`/library/${subject.id}/`, [tabs, subject.id, cards])
    const switcher = subject.courses.map(group => [group.course, group.courseSlug, group.notes.length])
    for (const group of subject.courses) {
      pages.set(`/courses/${group.courseSlug}/`, [subject.id, subject.name, group.course, switcher,
        group.notes.map(note => [note.slug, note.title, note.excerpt, note.readingMinutes])])
    }
  }
  const defaultSubject = subjects.find(subject => subject.courses.length) ?? subjects[0]
  pages.set('/library', defaultSubject ? pages.get(`/library/${defaultSubject.id}/`) : [tabs])
  return pages
}
