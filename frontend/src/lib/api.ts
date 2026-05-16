// Typed HTTP client. TanStack-Query-friendly: throws ApiError on non-2xx.
// Plan §step-3.

import { env } from "./env.client";

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly body: string,
  ) {
    super(`API ${status}: ${body.slice(0, 200)}`);
    this.name = "ApiError";
  }
}

export type ApiInit = Omit<RequestInit, "headers" | "body"> & {
  token?: string | null;
  headers?: Record<string, string>;
  body?: BodyInit | null;
};

export async function api<T>(path: string, init: ApiInit = {}): Promise<T> {
  const { token, headers, body, ...rest } = init;
  const res = await fetch(`${env.NEXT_PUBLIC_BACKEND_URL}${path}`, {
    ...rest,
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...headers,
    },
    body: body ?? undefined,
    cache: "no-store",
  } as RequestInit);
  if (!res.ok) {
    throw new ApiError(res.status, await res.text());
  }
  // 204 No Content / 201 with empty body — both legal.
  const text = await res.text();
  return (text ? JSON.parse(text) : (null as unknown)) as T;
}
