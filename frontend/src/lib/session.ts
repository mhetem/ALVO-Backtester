import { HttpError, errorMessage } from './api';

export type User = {
  id: string;
  email: string;
  created_at: string;
  updated_at: string;
};

type SessionResponse = {
  user: User;
  access_token: string;
  refresh_token: string;
  expires_in: number;
};

type AccessResponse = {
  user: User;
  access_token: string;
  expires_in: number;
};

let accessToken: string | null = null;

export function authorize(headers: Headers): Headers {
  if (accessToken) {
    headers.set('Authorization', `Bearer ${accessToken}`);
  }
  return headers;
}

async function postJSON<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(path, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new HttpError(res.status, await errorMessage(res));
  }
  return (await res.json()) as T;
}

export async function register(email: string, password: string): Promise<void> {
  await postJSON<User>('/api/v1/auth/register', { email, password });
}

export async function login(email: string, password: string): Promise<User> {
  const body = await postJSON<SessionResponse>('/api/v1/auth/login', { email, password });
  accessToken = body.access_token;
  return body.user;
}

export async function resume(): Promise<User | null> {
  const res = await fetch('/api/v1/auth/refresh', {
    method: 'POST',
    credentials: 'same-origin',
  });
  if (!res.ok) {
    accessToken = null;
    return null;
  }

  const body = (await res.json()) as AccessResponse;
  accessToken = body.access_token;
  return body.user;
}

export async function logout(): Promise<void> {
  accessToken = null;
  await fetch('/api/v1/auth/revoke', { method: 'POST', credentials: 'same-origin' });
}

export async function authorized(path: string, init: RequestInit, retry = true): Promise<Response> {
  const headers = authorize(new Headers(init.headers));
  const res = await fetch(path, { ...init, headers, credentials: 'same-origin' });

  if (res.status === 401 && retry && (await resume())) {
    return authorized(path, init, false);
  }

  return res;
}

export async function decode<T>(res: Response): Promise<T> {
  if (!res.ok) {
    throw new HttpError(res.status, await errorMessage(res));
  }
  return (await res.json()) as T;
}
