import type { AdminResponse } from './types';

let csrfToken = readCookie('apk_cache_admin_csrf');

export function setCSRF(value?: string) {
  csrfToken = value || readCookie('apk_cache_admin_csrf');
}

export function getCSRF() {
  return csrfToken;
}

export type APIInit = Omit<RequestInit, 'body'> & { body?: unknown };

export async function api<T>(path: string, init: APIInit = {}): Promise<T> {
  const response = await request(path, init);
  const payload = (await response.json().catch(() => ({
    ok: false,
    error: { message: response.statusText }
  }))) as AdminResponse<T>;
  if (!response.ok || !payload.ok) {
    throw new Error(payload.error?.message || response.statusText);
  }
  return payload.data;
}

export async function apiBlob(path: string, init: APIInit = {}): Promise<Blob> {
  const response = await request(path, init);
  if (!response.ok) {
    const payload = await response.json().catch(() => null);
    throw new Error(payload?.error?.message || response.statusText);
  }
  return response.blob();
}

async function request(path: string, init: APIInit) {
	const method = (init.method || 'GET').toUpperCase();
	const headers = new Headers(init.headers);
	headers.set('Accept', headers.get('Accept') || 'application/json');
	const { body: rawBody, ...rest } = init;
	let body: BodyInit | null | undefined;
	if (rawBody instanceof FormData || rawBody instanceof Blob || typeof rawBody === 'string') {
		body = rawBody;
	} else if (rawBody != null) {
		body = JSON.stringify(rawBody);
	}
	if (rawBody && typeof rawBody !== 'string' && !(rawBody instanceof FormData) && !(rawBody instanceof Blob)) {
		headers.set('Content-Type', 'application/json');
	}
	if (typeof body === 'string' && !headers.has('Content-Type')) {
		headers.set('Content-Type', 'application/json');
  }
  if (!['GET', 'HEAD'].includes(method) && csrfToken) {
    headers.set('X-CSRF-Token', csrfToken);
	}
	return fetch('/api/admin/v1' + path, {
		...rest,
		credentials: 'same-origin',
		headers,
		body
	});
}

function readCookie(name: string) {
  return document.cookie
    .split('; ')
    .find(item => item.startsWith(name + '='))
    ?.split('=')
    .slice(1)
    .join('=') || '';
}
