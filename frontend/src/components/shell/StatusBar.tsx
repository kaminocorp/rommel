export function StatusBar({ children }: { children: React.ReactNode }) {
  return (
    <footer className="flex h-7 items-center justify-end gap-3 border-t border-zinc-800 bg-zinc-950 px-4 text-[11px] text-zinc-400">
      {children}
    </footer>
  );
}
