"use client";

import { useState } from "react";
import { useSearchParams } from "next/navigation";
import { createBrowserClient } from "@/lib/auth";
import { Button } from "@/components/ui/button";

export default function SignInPage() {
  const params = useSearchParams();
  const next = params.get("next") ?? "/";
  const [email, setEmail] = useState("");
  const [status, setStatus] = useState<"idle" | "sending" | "sent" | "error">("idle");
  const [error, setError] = useState<string | null>(null);

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setStatus("sending");
    setError(null);
    const supabase = createBrowserClient();
    const redirectTo = `${window.location.origin}/auth/callback?next=${encodeURIComponent(next)}`;
    const { error: err } = await supabase.auth.signInWithOtp({
      email,
      options: { emailRedirectTo: redirectTo },
    });
    if (err) {
      setStatus("error");
      setError(err.message);
      return;
    }
    setStatus("sent");
  };

  return (
    <main className="mx-auto flex max-w-md flex-col gap-6 px-6 py-20">
      <div>
        <h1 className="text-2xl font-semibold">Sign in to Rommel</h1>
        <p className="text-sm text-zinc-400">We&apos;ll email you a one-time link.</p>
      </div>
      <form className="flex flex-col gap-3" onSubmit={onSubmit}>
        <label className="text-sm text-zinc-300">
          Email
          <input
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="mt-1 w-full rounded-md border border-zinc-700 bg-zinc-900 px-3 py-2 text-zinc-100 outline-none focus:border-zinc-500"
            placeholder="you@example.com"
          />
        </label>
        <Button type="submit" disabled={status === "sending"}>
          {status === "sending" ? "Sending…" : "Send magic link"}
        </Button>
        {status === "sent" && (
          <p className="text-sm text-emerald-400">Check your email for the link.</p>
        )}
        {status === "error" && error && <p className="text-sm text-red-400">{error}</p>}
      </form>
    </main>
  );
}
