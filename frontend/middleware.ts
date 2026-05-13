// Bouncer for unauthenticated traffic into protected routes.
// Plan §step-2.
//
// Why middleware (not a layout guard)? It runs before any RSC fetches the
// backend, so unauthenticated users never hit /workspaces and the backend
// never sees a Bearer-less call from this surface.

import { NextResponse, type NextRequest } from "next/server";
import { createMiddlewareSupabaseClient } from "@/lib/auth";

export async function middleware(req: NextRequest) {
  const res = NextResponse.next();
  const supabase = createMiddlewareSupabaseClient(req, res);
  const {
    data: { session },
  } = await supabase.auth.getSession();

  if (!session) {
    const signIn = new URL("/sign-in", req.url);
    signIn.searchParams.set("next", req.nextUrl.pathname + req.nextUrl.search);
    return NextResponse.redirect(signIn);
  }
  return res;
}

export const config = {
  matcher: ["/workspaces/:path*"],
};
