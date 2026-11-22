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

export async function api<T>(path: string, { json, file, ...options }: ApiRequest = {}): Promise<T> {
  const headers = new Headers(options.headers)
  headers.set('Accept', 'application/json')
  if (json !== undefined) headers.set('Content-Type', 'application/json')
  const response = await fetch(path, {
    ...options,
    headers,
    body: json === undefined ? file : JSON.stringify(json),
    credentials: 'same-origin',
  })
  const mediaType = response.headers.get('Content-Type')?.split(';', 1)[0].trim().toLowerCase()
  let data: unknown
  if (mediaType === 'application/json') {
    try {
      data = await response.json()
    } catch (error) {
      if (options.signal?.aborted || (error instanceof Error && error.name === 'AbortError')) throw error
    }
  }
  if (!response.ok) {
    const error = data as { error?: unknown; code?: unknown } | null | undefined
    const retryAfter = Number(response.headers.get('Retry-After'))
    throw new ApiError(
      typeof error?.error === 'string' ? error.error : `请求失败 (${response.status})`,
      response.status,
      typeof error?.code === 'string' ? error.code : '',
      Number.isFinite(retryAfter) && retryAfter > 0 ? retryAfter : 0,
    )
  }
  if (data === undefined) {
    throw new ApiError('服务器返回了无效的 JSON 响应', response.status, 'invalid_response')
  }
  return data as T
}
