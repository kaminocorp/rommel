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

type ReadonlyCookieStore = {
  get(name: string): { value: string } | undefined;
  set?(name: string, value: string, options?: CookieOptions): void;
};

export function createServerClient(cookieStore: ReadonlyCookieStore) {
  return createSupabaseServerClient(env.NEXT_PUBLIC_SUPABASE_URL, env.NEXT_PUBLIC_SUPABASE_ANON_KEY, {
    cookies: {
      get(name) {
        return cookieStore.get(name)?.value;
      },
      set(name, value, options) {
        // In RSC the `set` no-ops (cookies are read-only there); the route
        // handler / server action path injects a writable cookie store and
        // the writes actually land.
        cookieStore.set?.(name, value, options);
      },
      remove(name, options) {
        cookieStore.set?.(name, "", { ...options, maxAge: 0 });
      },
    },
  });
}

// --- Middleware ------------------------------------------------------------

export function createMiddlewareSupabaseClient(req: NextRequest, res: NextResponse) {
  return createSupabaseServerClient(env.NEXT_PUBLIC_SUPABASE_URL, env.NEXT_PUBLIC_SUPABASE_ANON_KEY, {
    cookies: {
      get(name) {
        return req.cookies.get(name)?.value;
      },
      set(name, value, options) {
        res.cookies.set({ name, value, ...options });
      },
      remove(name, options) {
        res.cookies.set({ name, value: "", ...options, maxAge: 0 });
      },
    },
  });
}

// Backend wants Authorization: Bearer <jwt>. The supabase-ssr session carries
// the JWT in `access_token`. Helper that callers use to attach it.
export async function getAccessTokenFromCookies(cookieStore: ReadonlyCookieStore): Promise<string | null> {
  const supabase = createServerClient(cookieStore);
  const { data } = await supabase.auth.getSession();
  return data.session?.access_token ?? null;
}
