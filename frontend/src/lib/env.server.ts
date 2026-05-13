// Server-only env. The "server-only" import causes Next to throw a build error
// if this module is ever pulled into a "use client" bundle — risk 4.5.
import "server-only";

import { z } from "zod";
import { env as publicEnv } from "./env.client";

const ServerEnv = z.object({
  SUPABASE_SERVICE_ROLE_KEY: z.string().min(1).optional(),
});

const parsed = ServerEnv.parse({
  SUPABASE_SERVICE_ROLE_KEY: process.env.SUPABASE_SERVICE_ROLE_KEY,
});

export const env = { ...publicEnv, ...parsed } as const;
