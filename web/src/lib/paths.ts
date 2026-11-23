const paths = typeof document === 'undefined' ? undefined : document.documentElement.dataset

export const siteBase = paths?.siteBase || import.meta.env.BASE_URL
export const apiBase = paths?.apiBase || import.meta.env.VITE_API_BASE_PATH
