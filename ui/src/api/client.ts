// Default to a relative, same-origin base. In dev the Vite proxy
// (vite.config.ts) forwards /api to joe-core; in prod the UI and API are
// served from the same origin. VITE_API_URL remains an explicit override
// for the rare cross-origin deployment.
export const API_BASE = (import.meta.env.VITE_API_URL as string | undefined) ?? '';

interface ApiError {
  error: string;
  message: string;
  details?: Record<string, unknown>;
}

// ApiRequestError is thrown for every non-2xx response. It extends Error so
// existing catch sites that only read `.message` keep working, while adding
// the HTTP `status` so callers (and the auth layer) can branch on 401 etc.
// without parsing the message string.
export class ApiRequestError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiRequestError';
    this.status = status;
    // Restore the prototype chain so `instanceof ApiRequestError` holds when
    // compiled to ES targets that break subclassing of built-ins.
    Object.setPrototypeOf(this, ApiRequestError.prototype);
  }
}

class ApiClient {
  private token: string | null = null;
  private onUnauthorized: (() => void) | null = null;

  setToken(token: string) {
    this.token = token;
  }

  clearToken() {
    this.token = null;
  }

  // setUnauthorizedHandler registers a single callback invoked whenever any
  // request returns 401. The auth layer uses it to flip the app to its
  // logged-out state from anywhere, without each call site handling 401.
  setUnauthorizedHandler(fn: (() => void) | null) {
    this.onUnauthorized = fn;
  }

  async request<T>(path: string, options: RequestInit = {}): Promise<T> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      ...(options.headers as Record<string, string>),
    };

    if (this.token) {
      headers.Authorization = `Bearer ${this.token}`;
    }

    const response = await fetch(`${API_BASE}${path}`, {
      ...options,
      headers,
      // Send the joe_session cookie on every request. Same-origin would send
      // it by default, but 'include' also covers the cross-origin
      // VITE_API_URL override so the human (cookie) session resolves there
      // too. The conditional Authorization bearer header above is untouched,
      // so cookie (human) and bearer (break-glass) coexist.
      credentials: 'include',
    });

    if (!response.ok) {
      let errMsg = 'API request failed';
      try {
        const err = (await response.json()) as ApiError;
        errMsg = err.message || errMsg;
      } catch {
        // ignore parse error
      }
      if (response.status === 401) {
        this.onUnauthorized?.();
      }
      throw new ApiRequestError(response.status, errMsg);
    }

    return response.json() as Promise<T>;
  }

  // requestRaw performs a fetch with the same base URL and auth as request()
  // — cookie (credentials: 'include') plus the conditional Authorization
  // bearer header — but returns the raw Response without parsing JSON or
  // throwing on non-2xx. Streaming callers (SSE) need the live ReadableStream
  // body and must inspect a possible pre-stream non-200 JSON error themselves.
  // A 401 still trips the logged-out transition so an expired session logs out
  // on a streamed turn exactly as it does on any other request.
  async requestRaw(path: string, options: RequestInit = {}): Promise<Response> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      ...(options.headers as Record<string, string>),
    };

    if (this.token) {
      headers.Authorization = `Bearer ${this.token}`;
    }

    const response = await fetch(`${API_BASE}${path}`, {
      ...options,
      headers,
      credentials: 'include',
    });

    if (response.status === 401) {
      this.onUnauthorized?.();
    }

    return response;
  }

  get<T>(path: string): Promise<T> {
    return this.request<T>(path, { method: 'GET' });
  }

  post<T>(path: string, body: unknown): Promise<T> {
    return this.request<T>(path, {
      method: 'POST',
      body: JSON.stringify(body),
    });
  }

  put<T>(path: string, body: unknown): Promise<T> {
    return this.request<T>(path, {
      method: 'PUT',
      body: JSON.stringify(body),
    });
  }

  patch<T>(path: string, body: unknown): Promise<T> {
    return this.request<T>(path, {
      method: 'PATCH',
      body: JSON.stringify(body),
    });
  }

  delete<T>(path: string): Promise<T> {
    return this.request<T>(path, { method: 'DELETE' });
  }
}

export const apiClient = new ApiClient();
