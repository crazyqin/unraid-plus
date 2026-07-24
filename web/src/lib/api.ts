/**
 * Lightweight API client. Throws an `ApiError` (with status + body) on non-2xx
 * so React Query can surface it via `error`.
 */
import i18n from '@/i18n';
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

/** v0.8+: Active server ID for multi-server data isolation. */
let _activeServerId: string | null = null;

/** Set the active server ID (called by auth store). All subsequent API
 *  requests will include `?serverId=` so the backend routes to the
 *  correct SSH connection. */
export function setActiveServerId(id: string | null) {
  _activeServerId = id;
}

/** Inject ?serverId= into paths that need it (data endpoints, not auth/connect). */
function withServerId(path: string): string {
  if (!_activeServerId) return path;
  // Don't add serverId to auth/connect/server-management routes
  if (path.startsWith('/auth/') || path === '/connect' || path === '/disconnect' || path.startsWith('/servers')) {
    return path;
  }
  const sep = path.includes('?') ? '&' : '?';
  return `${path}${sep}serverId=${encodeURIComponent(_activeServerId)}`;
}

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
  const resolvedPath = withServerId(path);
  const res = await fetch(`${BASE}${resolvedPath}`, {
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
    const res = await fetch(`${BASE}${withServerId(path)}`, {
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
   * Upload files via multipart/form-data with progress tracking.
   * Uses XHR instead of fetch() because fetch() does not support
   * upload progress events. Returns a promise that resolves with
   * the parsed response (JSON or text).
   */
  uploadWithProgress: <T>(
    path: string,
    formData: FormData,
    onProgress?: (loaded: number, total: number) => void,
  ): Promise<T> => {
    return new Promise<T>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', `${BASE}${withServerId(path)}`);
      xhr.withCredentials = true;

      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable && onProgress) {
          onProgress(e.loaded, e.total);
        }
      };

      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) {
          const ct = xhr.getResponseHeader('content-type') ?? '';
          if (ct.includes('application/json')) {
            resolve(JSON.parse(xhr.responseText) as T);
          } else {
            resolve(xhr.responseText as unknown as T);
          }
        } else {
          let body: unknown = null;
          try {
            body = xhr.responseText ? JSON.parse(xhr.responseText) : null;
          } catch {
            body = xhr.responseText;
          }
          const msg =
            (body && typeof body === 'object' && 'message' in body
              ? String((body as { message: unknown }).message)
              : undefined) ?? xhr.statusText ?? 'upload failed';
          handleAuthRedirect(xhr.status, body);
          reject(new ApiError(msg, xhr.status, body));
        }
      };

      xhr.onerror = () => reject(new ApiError(i18n.t('api.networkError'), 0, null));
      xhr.send(formData);
    });
  },

  /**
   * Fetch a file preview (first 64KB) from the backend. Returns the
   * response object so the caller can inspect Content-Type and
   * X-Preview-Truncated headers.
   */
  preview: async (path: string): Promise<Response> => {
    const resolvedPath = withServerId(`/files/preview?path=${encodeURIComponent(path)}`);
    const res = await fetch(
      `${BASE}${resolvedPath}`,
      { credentials: 'include' },
    );
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
          : undefined) ?? res.statusText ?? 'preview failed';
      handleAuthRedirect(res.status, body);
      throw new ApiError(msg, res.status, body);
    }
    return res;
  },

  /**
   * Build a download URL for same-origin file download. The browser handles
   * the Content-Disposition header and saves the file. Cookies are sent
   * automatically for same-origin navigation.
   */
  downloadUrl: (path: string): string => `${BASE}${withServerId(path)}`,

  /**
   * Save text content to a remote file. Used by the file editor to write
   * changes back to the Unraid host via SFTP.
   */
  saveFileContent: (filePath: string, content: string) =>
    request<{ ok: boolean; message: string }>('/files/save', {
      method: 'POST',
      body: JSON.stringify({ path: filePath, content }),
    }),
};

/** Build a WebSocket URL relative to the current origin.
 *  Injects ?serverId= for multi-server data routes when active. */
export function wsUrl(path: string): string {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  let p = path;
  if (_activeServerId && !p.includes('serverId=')) {
    const sep = p.includes('?') ? '&' : '?';
    p = `${p}${sep}serverId=${encodeURIComponent(_activeServerId)}`;
  }
  return `${proto}//${window.location.host}${p}`;
}
