export interface Heading {
  level: number
  title: string
  slug: string
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

export interface MathFrame {
  width: number
  height: number
  align: number
  space?: number
  break?: 'soft' | 'hard'
  layout?: number
  view?: string
}

export interface MathEntry {
  tex: string
  display: boolean
  frames?: MathFrame[]
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
