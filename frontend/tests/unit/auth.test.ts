// Sanity tests for the auth helper signatures. The full sign-in flow is
// integration-tested by the Playwright spec; this file checks the helper
// surfaces resolve to functions and don't accidentally import server-only
// modules.

import { describe, it, expect } from "vitest";

describe("auth helpers shape", () => {
  it("env.client exposes only NEXT_PUBLIC_*", async () => {
    // Inject the public vars before importing the module.
    process.env.NEXT_PUBLIC_BACKEND_URL = "http://localhost:8080";
    process.env.NEXT_PUBLIC_SUPABASE_URL = "https://example.supabase.co";
    process.env.NEXT_PUBLIC_SUPABASE_ANON_KEY = "anon-key";
    const mod = await import("@/lib/env.client");
    expect(mod.env.NEXT_PUBLIC_BACKEND_URL).toBe("http://localhost:8080");
    expect(mod.env.NEXT_PUBLIC_SUPABASE_URL).toBe("https://example.supabase.co");
    expect(mod.env.NEXT_PUBLIC_SUPABASE_ANON_KEY).toBe("anon-key");
    expect("SUPABASE_SERVICE_ROLE_KEY" in mod.env).toBe(false);
  });

  it("auth module exports the three client factories", async () => {
    const mod = await import("@/lib/auth");
    expect(typeof mod.createBrowserClient).toBe("function");
    expect(typeof mod.createServerClient).toBe("function");
    expect(typeof mod.createMiddlewareSupabaseClient).toBe("function");
    expect(typeof mod.getAccessTokenFromCookies).toBe("function");
  });
});
