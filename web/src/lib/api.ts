import { apiBase } from './paths'

export class ApiError extends Error {
  status: number
  code: string
  retryAfter: number

  constructor(message: string, status: number, code: string, retryAfter = 0) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.retryAfter = retryAfter
  }
}

type ApiRequest = Omit<RequestInit, 'body'> &
  ({ json?: unknown; file?: never } | { json?: never; file: Blob })

type ApiPath = `/${string}`

export function apiUrl(path: ApiPath): string {
  return `${apiBase}${path.slice(1)}`
}

async function requestApi(path: ApiPath, { json, file, ...options }: ApiRequest, accept: string) {
  const headers = new Headers(options.headers)
  headers.set('Accept', accept)
  if (json !== undefined) headers.set('Content-Type', 'application/json')
  const response = await fetch(apiUrl(path), {
    ...options,
    headers,
    body: json === undefined ? file : JSON.stringify(json),
    credentials: 'same-origin',
  })
  if (response.ok) return response
  const data = await responseJSON(response, options.signal) as { error?: unknown; code?: unknown } | undefined
  const retryAfter = Number(response.headers.get('Retry-After'))
  throw new ApiError(
    typeof data?.error === 'string' ? data.error : `请求失败 (${response.status})`,
    response.status,
    typeof data?.code === 'string' ? data.code : '',
    Number.isFinite(retryAfter) && retryAfter > 0 ? retryAfter : 0,
  )
}

async function responseJSON(response: Response, signal?: AbortSignal | null) {
  const mediaType = response.headers.get('Content-Type')?.split(';', 1)[0].trim().toLowerCase()
  let data: unknown
  if (mediaType === 'application/json') {
    try {
      data = await response.json()
    } catch (error) {
      if (signal?.aborted || (error instanceof Error && error.name === 'AbortError')) throw error
    }
  }
  return data
}

export async function api<T>(path: ApiPath, options: ApiRequest = {}): Promise<T> {
  const response = await requestApi(path, options, 'application/json')
  const data = await responseJSON(response, options.signal)
  if (data === undefined) {
    throw new ApiError('服务器返回了无效的 JSON 响应', response.status, 'invalid_response')
  }
  return data as T
}

export async function apiZip(path: ApiPath, options: ApiRequest): Promise<Blob> {
  const response = await requestApi(path, options, 'application/zip')
  if (response.headers.get('Content-Type')?.split(';', 1)[0] !== 'application/zip') {
    throw new ApiError('服务器未返回文档包', response.status, 'invalid_response')
  }
  return response.blob()
}
