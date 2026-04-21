/**
 * Authed fetch helper. Injects `Authorization: Bearer <token>` from
 * localStorage on every call. On 401, throws `UnauthorizedError` so
 * AuthProvider can clear the token and redirect to /auth?next=…
 */

export const TOKEN_KEY = "ctm.token";

export class UnauthorizedError extends Error {
  override name = "UnauthorizedError";
  constructor(message = "Unauthorized") {
    super(message);
  }
}

export class ApiError extends Error {
  override name = "ApiError";
  constructor(
    public status: number,
    message: string,
    public body?: unknown,
  ) {
    super(message);
  }
}

export function getToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY);
  } catch {
    return null;
  }
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token);
}

export function clearToken(): void {
  try {
    localStorage.removeItem(TOKEN_KEY);
  } catch {
    /* ignore */
  }
}

interface ApiOptions extends RequestInit {
  /** Override the bearer token for this call (used during initial paste). */
  token?: string;
}

export async function api<T = unknown>(
  path: string,
  opts: ApiOptions = {},
): Promise<T> {
  const { token: overrideToken, ...rest } = opts;
  const token = overrideToken ?? getToken();

  const headers = new Headers(rest.headers);
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (rest.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  headers.set("Accept", "application/json");

  const res = await fetch(path, { ...rest, headers });

  if (res.status === 401) {
    throw new UnauthorizedError(`401 Unauthorized: ${path}`);
  }
  if (!res.ok) {
    let body: unknown = undefined;
    try {
      body = await res.json();
    } catch {
      try {
        body = await res.text();
      } catch {
        /* ignore */
      }
    }
    throw new ApiError(res.status, `${res.status} ${res.statusText}: ${path}`, body);
  }

  if (res.status === 204) return undefined as T;

  const contentType = res.headers.get("content-type") ?? "";
  if (contentType.includes("application/json")) {
    return (await res.json()) as T;
  }
  return (await res.text()) as unknown as T;
}

/** Build authorization header for SSE / fetch-event-source consumers. */
export function authHeaders(): Record<string, string> {
  const token = getToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

/* -------------------------------------------------------------------------- */
/* Revert API                                                                 */
/* -------------------------------------------------------------------------- */

export interface RevertRequest {
  sha: string;
  stash_first?: boolean;
}

export interface RevertSuccess {
  ok: true;
  reverted_to: string;
  stashed_as?: string;
}

export interface RevertDirty {
  error: "dirty_workdir" | string;
  dirty_files: string[];
}

/**
 * POST /api/sessions/:name/revert. Returns the parsed success payload on
 * 200; on 409 the caller catches `ApiError` and inspects `body` (typed
 * as RevertDirty). 422 indicates the SHA isn't in the server-side
 * allowlist (should not happen if caller passes a SHA from the sibling
 * /checkpoints response).
 */
export async function postRevert(
  sessionName: string,
  body: RevertRequest,
): Promise<RevertSuccess> {
  return api<RevertSuccess>(
    `/api/sessions/${encodeURIComponent(sessionName)}/revert`,
    {
      method: "POST",
      body: JSON.stringify(body),
    },
  );
}
