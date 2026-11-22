import defaults from '../../../internal/app/default_categories.json' with { type: 'json' }

export interface Category { name: string; description: string }
export type Categories = Record<string, Category>

// IDs are shared by public URLs and content directories on Windows and Unix.
export function validCategoryID(id: string) {
  return /^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(id)
    && !/^(?:con|prn|aux|nul|com[1-9]|lpt[1-9]|constructor|prototype)$/.test(id)
}

export function readCategories(value: unknown): Categories {
  const categories: Categories = Object.fromEntries(Object.entries(defaults).map(([id, entry]) => [id, { ...entry }]))
  if (!value || typeof value !== 'object' || Array.isArray(value)) return categories
  for (const [id, entry] of Object.entries(value)) {
    if (!validCategoryID(id) || !entry || typeof entry !== 'object') continue
    categories[id] = {
      name: typeof entry.name === 'string' && entry.name.trim() ? entry.name.trim() : categories[id]?.name || id,
      description: typeof entry.description === 'string' ? entry.description : categories[id]?.description || '',
    }
  }
  return categories
}

export function withCatalogCategories(categories: Categories, notes: { category: string; categoryTitle?: string }[]): Categories {
  const result = { ...categories }
  for (const note of notes) {
    if (validCategoryID(note.category) && !Object.hasOwn(result, note.category)) {
      result[note.category] = { name: note.categoryTitle || note.category, description: '' }
    }
  }
  return result
}
