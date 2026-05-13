// Client-safe env. Bundled into the browser at build time. Only the four
// NEXT_PUBLIC_* names below may live here — risk 4.5 in the Phase-5 plan.
//
// Server-only secrets (e.g. SUPABASE_SERVICE_ROLE_KEY) belong in env.server.ts,
// which is import-restricted via ESLint (eslint.config.mjs) so client code
// can't reach them accidentally.

import { z } from "zod";

const ClientEnv = z.object({
  NEXT_PUBLIC_BACKEND_URL: z.string().url(),
  NEXT_PUBLIC_SUPABASE_URL: z.string().url(),
  NEXT_PUBLIC_SUPABASE_ANON_KEY: z.string().min(1),
});

// Pluck only the keys we declare — Next inlines `process.env.NEXT_PUBLIC_*`
// statically at build, so direct destructuring keeps tree-shaking working.
export const env = ClientEnv.parse({
  NEXT_PUBLIC_BACKEND_URL: process.env.NEXT_PUBLIC_BACKEND_URL,
  NEXT_PUBLIC_SUPABASE_URL: process.env.NEXT_PUBLIC_SUPABASE_URL,
  NEXT_PUBLIC_SUPABASE_ANON_KEY: process.env.NEXT_PUBLIC_SUPABASE_ANON_KEY,
});
