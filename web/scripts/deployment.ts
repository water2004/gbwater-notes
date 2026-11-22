export const sitePlaceholder = '/GBWATER_BASE_PATH_PLACEHOLDER/'
export const apiPlaceholder = '/GBWATER_API_BASE_PATH_PLACEHOLDER/'
export const dynamicAssets = 'admin-assets'
export const dynamicEntries = new Set(['admin', 'login', 'register', 'forgot', dynamicAssets])

export function isDynamicPath(path: string): boolean {
  return dynamicEntries.has(path.replace(/^\//, '').split('/', 1)[0])
}

function directory(value: string, name: string): string {
  const path = `/${value.trim().replace(/^\/+|\/+$/g, '')}`
  if (!/^\/(?:[\w.~-]+(?:\/[\w.~-]+)*)?$/.test(path) || /(?:^|\/)\.{1,2}(?:\/|$)/.test(path)) {
    throw new Error(`${name} 必须是站内路径，例如 /mypdf/`)
  }
  return path === '/' ? path : `${path}/`
}

export function deploymentPaths(env: Record<string, string | undefined>) {
  const site = directory(env.GBWATER_BASE_PATH?.trim() || '/mypdf/', 'GBWATER_BASE_PATH')
  const api = directory(env.GBWATER_API_BASE_PATH?.trim() || `${site}api/`, 'GBWATER_API_BASE_PATH')
  if (site.startsWith(api)) throw new Error('API 前缀须与站点页面路径区分')
  return { site, api }
}
