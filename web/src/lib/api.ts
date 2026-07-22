/**
 * Lightweight API client. Throws an `ApiError` (with status + body) on non-2xx
 * so React Query can surface it via `error`.
 */
export class ApiError extends Error {
  status: number;
  body: unknown;
  constructor(message: string, status: number, body: unknown) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.body = body;
  }
}

const BASE = '/api';

/**
 * Handle 401 AUTH_REQUIRED responses by redirecting to /login.
 * Skips the redirect if we're already on /login (to avoid loops).
 * This is called from the request() function below.
 */
function handleAuthRedirect(status: number, body: unknown): void {
  if (status !== 401) return;
  if (
    body &&
    typeof body === 'object' &&
    'code' in body &&
    (body as { code: unknown }).code === 'AUTH_REQUIRED'
  ) {
    if (window.location.pathname !== '/login') {
      window.location.href = '/login';
    }
  }
}

async function request<T>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(init.headers || {}),
    },
    credentials: 'include',
  });

  if (!res.ok) {
    let body: unknown = null;
    const text = await res.text();
    try {
      body = text ? JSON.parse(text) : null;
    } catch {
      body = text;
    }
    const msg =
      (body && typeof body === 'object' && 'message' in body
        ? String((body as { message: unknown }).message)
        : undefined) ?? res.statusText ?? 'request failed';
    handleAuthRedirect(res.status, body);
    throw new ApiError(msg, res.status, body);
  }

  if (res.status === 204) return undefined as T;
  const ct = res.headers.get('content-type') ?? '';
  if (ct.includes('application/json')) return (await res.json()) as T;
  return (await res.text()) as unknown as T;
}

export const api = {
  get: <T>(path: string) => request<T>(path, { method: 'GET' }),
  post: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'POST', body: body ? JSON.stringify(body) : undefined }),
  put: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'PUT', body: body ? JSON.stringify(body) : undefined }),
  delete: <T>(path: string) => request<T>(path, { method: 'DELETE' }),

  /**
   * Upload files via multipart/form-data. The browser sets the Content-Type
   * header automatically (including the boundary), so we must NOT set it
   * manually — that's why this is a separate method, not using request().
   */
  upload: async <T>(path: string, formData: FormData): Promise<T> => {
    const res = await fetch(`${BASE}${path}`, {
      method: 'POST',
      body: formData,
      credentials: 'include',
    });
    if (!res.ok) {
      let body: unknown = null;
      const text = await res.text();
      try {
        body = text ? JSON.parse(text) : null;
      } catch {
        body = text;
      }
      const msg =
        (body && typeof body === 'object' && 'message' in body
          ? String((body as { message: unknown }).message)
          : undefined) ?? res.statusText ?? 'upload failed';
      handleAuthRedirect(res.status, body);
      throw new ApiError(msg, res.status, body);
    }
    const ct = res.headers.get('content-type') ?? '';
    if (ct.includes('application/json')) return (await res.json()) as T;
    return (await res.text()) as unknown as T;
  },

  /**
   * Build a download URL for same-origin file download. The browser handles
   * the Content-Disposition header and saves the file. Cookies are sent
   * automatically for same-origin navigation.
   */
  downloadUrl: (path: string): string => `${BASE}${path}`,
};

/** Build a WebSocket URL relative to the current origin. */
export function wsUrl(path: string): string {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${proto}//${window.location.host}${path}`;
}
