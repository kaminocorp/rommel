// OAuth / magic-link code-exchange handler.
// The Supabase email link sends the user back here with `?code=…` after they
// click. We exchange it for a session, the session lands as cookies, and we
// bounce to `next` (or `/`).

import { cookies } from "next/headers";
import { NextResponse, type NextRequest } from "next/server";
import { createServerClient } from "@/lib/auth";

export async function GET(req: NextRequest) {
  const url = new URL(req.url);
  const code = url.searchParams.get("code");
  const next = url.searchParams.get("next") ?? "/";

  if (!code) {
    return NextResponse.redirect(new URL("/sign-in?error=missing_code", url));
  }

  const cookieStore = await cookies();
  const supabase = createServerClient(cookieStore);
  const { error } = await supabase.auth.exchangeCodeForSession(code);
  if (error) {
    const back = new URL("/sign-in", url);
    back.searchParams.set("error", error.message);
    return NextResponse.redirect(back);
  }
  return NextResponse.redirect(new URL(next, url));
}
