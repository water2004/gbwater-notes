import { ref } from 'vue'
import type { NoteMeta } from '@/types/content'
import { withCatalogCategories } from './categories'
import { groupCourses } from './course-groups'
import { siteSettings } from './siteSettings'

export let publishedNotes: NoteMeta[] = []
export let categories = siteSettings.categories
export const catalogUnavailable = ref(false)

export function initializeCatalog(notes: NoteMeta[]) {
  publishedNotes = notes.filter(note => !note.draft)
  categories = withCatalogCategories(siteSettings.categories, publishedNotes)
}

export function categoryName(id: string, fallback?: string) {
  return categories[id]?.name || fallback || id
}

export function notePath(note: Pick<NoteMeta, 'slug'> | string): string {
  const slug = typeof note === 'string' ? (getNote(note)?.slug || note) : note.slug
  return `/notes/${encodeURIComponent(slug)}/`
}

export function coursePath(note: Pick<NoteMeta, 'courseSlug'>): string {
  return `/courses/${encodeURIComponent(note.courseSlug)}/`
}

export function notesForCourse(category: string, course: string): NoteMeta[] {
  return publishedNotes
    .filter(note => note.category === category && note.course === course)
    .sort((a, b) => a.order - b.order || a.title.localeCompare(b.title, 'zh-CN'))
}

export function getNote(idOrSlug: string): NoteMeta | undefined {
  return publishedNotes.find(note => note.id === idOrSlug || note.slug === idOrSlug)
}

export function getCourses(category?: string) {
  return groupCourses(publishedNotes, category)
}

export { headingSlug } from '@/lib/slug'
