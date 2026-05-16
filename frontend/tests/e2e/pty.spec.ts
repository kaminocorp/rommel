// Phase-0 live production gate (extends Phase-5 ping.spec.ts).
//
// Verifies the full PTY streaming path over real (or local) infrastructure:
//   1. Sign in (Supabase test user)
//   2. Workspace page loads, connection-pill → "ready"
//   3. Terminal pane bootstraps a real PTY (mounting… → opening… → ready)
//   4. Keystrokes flow (pty.input) and output renders (pty.output events)
//   5. Exit is handled cleanly (pty.exit event → status "exited", footer text)
//
// The xterm canvas makes full text-content assertions brittle without extra
// test hooks, so this spec focuses on the observable contract:
//   - data-testid="terminal-status" + data-state transitions
//   - typing `exit\r` produces the exited state with a visible footer
//
// Run locally (three terminals):
//   T1: ROMMEL_WORKSPACE_ROOT=$(pwd) make -C sandbox-daemon run-local
//   T2: docker compose -f backend/compose.yaml up -d postgres && make -C backend migrate run
//   T3: pnpm --filter ./frontend dev
// Then: pnpm --filter ./frontend test:e2e -- tests/e2e/pty.spec.ts
//
// In CI / production verification: point at the deployed Vercel URL + real
// workspace + real Fly machine (E2E_* secrets + vars point at prod Supabase
// and the live backend). No local daemon/backend startup in that mode.

import { test, expect, type Page } from "@playwright/test";

async function signInAsTestUser(page: Page): Promise<void> {
  const email = process.env.E2E_TEST_EMAIL;
  const password = process.env.E2E_TEST_PASSWORD;
  const supabaseUrl = process.env.NEXT_PUBLIC_SUPABASE_URL;
  const supabaseAnonKey = process.env.NEXT_PUBLIC_SUPABASE_ANON_KEY;
  if (!email || !password || !supabaseUrl || !supabaseAnonKey) {
    test.skip(true, "E2E_TEST_EMAIL / PASSWORD / Supabase env not set; skipping PTY gate.");
    return;
  }

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

test("integration: sign-in → workspace → terminal ready → type + exit", async ({ page }) => {
  await signInAsTestUser(page);

  const workspaceId = process.env.E2E_WORKSPACE_ID ?? "dev-workspace";
  await page.goto(`/workspaces/${workspaceId}`);

  // Connection must be ready before the PTY will open (usePty gates on daemon ready).
  await expect(page.getByTestId("connection-pill")).toHaveAttribute("data-status", "ready", {
    timeout: 20_000,
  });

  // Terminal status strip (xterm-impl.tsx). Initial mount is "mounting…", then
  // the usePty hook drives it through "opening…" → "ready".
  const status = page.getByTestId("terminal-status");
  await expect(status).toHaveAttribute("data-state", "ready", { timeout: 25_000 });

  // Focus the xterm host and type a command that will exit cleanly.
  // The host div is the container for the xterm canvas + viewport.
  const terminalHost = page.locator(".xterm-host").first();
  await terminalHost.click();
  await page.keyboard.type("exit 0\r");

  // After the shell exits, the PTY handler publishes pty.exit and the UI
  // renders the dimmed footer + flips disableStdin. The data-state becomes "exited".
  await expect(status).toHaveAttribute("data-state", "exited", { timeout: 10_000 });

  // The indicator text (visible in the status bar) contains the exit code.
  await expect(status).toContainText(/exited \(code 0\)/);

  // Optional: refresh the page — a fresh PTY should come up (no zombie on daemon side).
  // This also exercises the reconnect path in useDaemonConnection + usePty.
  await page.reload();
  await expect(page.getByTestId("connection-pill")).toHaveAttribute("data-status", "ready", {
    timeout: 15_000,
  });
  await expect(status).toHaveAttribute("data-state", "ready", { timeout: 20_000 });
});
