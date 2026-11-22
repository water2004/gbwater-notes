import { api } from './api'
import { readCategories } from './categories'

export async function fetchAdminCategories() {
  const settings = await api<{ categories?: unknown }>('/api/admin/site-settings')
  return readCategories(settings.categories)
}
