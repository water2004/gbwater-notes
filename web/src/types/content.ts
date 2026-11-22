export interface Heading {
  level: number
  title: string
}

export interface NoteMeta {
  id: string
  title: string
  category: string
  categoryTitle: string
  course: string
  slug: string
  courseSlug: string
  order: number
  draft: boolean
  excerpt: string
  characters: number
  readingMinutes: number
  headings: Heading[]
}

export interface MathEntry {
  tex: string
  display: boolean
  width?: number
  height?: number
  baseline?: number
  error?: string
}

export interface MermaidEntry {
  source: string
  width?: number
  height?: number
  error?: string
}

export interface NativeDocument {
  math: Record<string, MathEntry>
  mermaid: Record<string, MermaidEntry>
}

export interface SearchResult {
  id: string
  slug: string
  title: string
  course: string
  category: string
  excerpt: string
  score: number
}
