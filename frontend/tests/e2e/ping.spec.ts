// Phase-5 integration gate. Plan §step-6.
//
// Prerequisites (local & CI):
//   - sandbox-daemon running on :7777 with a known EdDSA pubkey
//   - backend running on :8080 with the matching priv key + a stubbed user
//   - frontend running on :3000, env wired at .env.local
//
// The spec asserts the connection pill reaches "ready" after the workspace
// page mounts. The pill flips to ready only after:
//   1. POST /workspaces/:id/sessions succeeds (backend signs)
//   2. WS opens against ws://localhost:7777/ws?token=... (daemon verifies)
//   3. system.ping round-trips
//
// Test-user sign-in is intentionally pluggable: in CI we use a programmatic
// Supabase password sign-in via the API; the helper below assumes that
// environment.

import { test, expect, type Page } from "@playwright/test";

async function signInAsTestUser(page: Page): Promise<void> {
  const email = process.env.E2E_TEST_EMAIL;
  const password = process.env.E2E_TEST_PASSWORD;
  const supabaseUrl = process.env.NEXT_PUBLIC_SUPABASE_URL;
  const supabaseAnonKey = process.env.NEXT_PUBLIC_SUPABASE_ANON_KEY;
  if (!email || !password || !supabaseUrl || !supabaseAnonKey) {
    test.skip(true, "E2E_TEST_EMAIL / PASSWORD / Supabase env not set; skipping integration gate.");
    return;
  }

  // Programmatic sign-in via Supabase's REST endpoint, then plant the session
  // cookie that @supabase/ssr expects. Avoids the magic-link round-trip.
  const res = await page.request.post(
    `${supabaseUrl}/auth/v1/token?grant_type=password`,
    {
      headers: { apikey: supabaseAnonKey, "Content-Type": "application/json" },
      data: { email, password },
    },
  );
  if (!res.ok()) throw new Error(`supabase sign-in failed: ${await res.text()}`);
  const session = (await res.json()) as { access_token: string; refresh_token: string };
  const cookieValue = JSON.stringify([session.access_token, session.refresh_token, null, null, null]);
  await page.context().addCookies([
    {
      name: `sb-${new URL(supabaseUrl).hostname.split(".")[0]}-auth-token`,
      value: cookieValue,
      domain: "localhost",
      path: "/",
      httpOnly: false,
      secure: false,
      sameSite: "Lax",
    },
  ]);
}

test("integration: sign-in → workspace page → connection-pill ready", async ({ page }) => {
  await signInAsTestUser(page);
  await page.goto("/");
  // The landing page is the entrypoint; the workspace id is provided via env
  // or created on the fly.
  const workspaceId = process.env.E2E_WORKSPACE_ID ?? "dev-workspace";
  await page.goto(`/workspaces/${workspaceId}`);
  await expect(page.getByTestId("connection-pill")).toHaveAttribute("data-status", "ready", {
    timeout: 15_000,
  });
});
