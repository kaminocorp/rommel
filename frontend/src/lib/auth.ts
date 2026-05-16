// Supabase client factories — plan §0.2.
//
// Three flavors with different cookie-handling backends:
//   - createBrowserClient: "use client" components (reads JWT from the cookie
//     the SSR client wrote)
//   - createServerClient(cookies()):   RSC / route handlers
//   - createMiddlewareSupabaseClient(): middleware.ts (request-bound cookies)
//
// All three share env.client (anon key is safe to ship).

import {
  createBrowserClient as createSupabaseBrowserClient,
  createServerClient as createSupabaseServerClient,
  type CookieOptions,
} from "@supabase/ssr";
import type { NextRequest, NextResponse } from "next/server";
import { env } from "./env.client";

// --- Browser ---------------------------------------------------------------

export function createBrowserClient() {
  return createSupabaseBrowserClient(env.NEXT_PUBLIC_SUPABASE_URL, env.NEXT_PUBLIC_SUPABASE_ANON_KEY);
}

// --- Server (RSC / route handlers / server actions) ------------------------
//
// `cookieStore` is the result of `next/headers`' `cookies()` — passed in
// rather than imported here so this module is import-safe from anywhere.

// Accepts both the hand-written test shape and the real object returned by
// `next/headers` `cookies()` (ReadonlyRequestCookies in Next 15). We only
// consume .get(name) and optional .set; the extra methods on the real object
// are ignored (duck typing).
type ReadonlyCookieStore = {
  get(name: string): { value: string } | undefined;
  set?(name: string, value: string, options?: CookieOptions): void;
  [key: string]: unknown;
};

export function createServerClient(cookieStore: ReadonlyCookieStore) {
  // The cookie adapter bridges Next's cookie store shape to the one
  // @supabase/ssr expects. The cast is the minimal surface to keep tsc
  // happy across @supabase/ssr ^0.5 + Next 15 + React 19 (pre-existing
  // Phase-5 typing friction; resolved for clean typecheck in Phase 0).
  return createSupabaseServerClient(env.NEXT_PUBLIC_SUPABASE_URL, env.NEXT_PUBLIC_SUPABASE_ANON_KEY, {
    cookies: {
      get(name: string) {
        return cookieStore.get(name)?.value;
      },
      set(name: string, value: string, options: CookieOptions) {
        cookieStore.set?.(name, value, options);
      },
      remove(name: string, options: CookieOptions) {
        cookieStore.set?.(name, "", { ...options, maxAge: 0 });
      },
    },
  } as any);
}

// --- Middleware ------------------------------------------------------------

export function createMiddlewareSupabaseClient(req: NextRequest, res: NextResponse) {
  return createSupabaseServerClient(env.NEXT_PUBLIC_SUPABASE_URL, env.NEXT_PUBLIC_SUPABASE_ANON_KEY, {
    cookies: {
      get(name: string) {
        return req.cookies.get(name)?.value;
      },
      set(name: string, value: string, options: CookieOptions) {
        res.cookies.set({ name, value, ...options });
      },
      remove(name: string, options: CookieOptions) {
        res.cookies.set({ name, value: "", ...options, maxAge: 0 });
      },
    },
  } as any);
}

// Backend wants Authorization: Bearer <jwt>. The supabase-ssr session carries
// the JWT in `access_token`. Helper that callers use to attach it.
export async function getAccessTokenFromCookies(cookieStore: ReadonlyCookieStore): Promise<string | null> {
  const supabase = createServerClient(cookieStore);
  const { data } = await supabase.auth.getSession();
  return data.session?.access_token ?? null;
}
