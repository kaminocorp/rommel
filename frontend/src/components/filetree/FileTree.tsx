// Stub file tree. Phase 5 ships a placeholder; Phase 6 wires it to fs.list +
// fs.watch via the daemon RPC.

export function FileTree() {
  return (
    <div className="p-3 text-xs text-zinc-500">
      <p className="mb-2 font-medium uppercase tracking-wide text-zinc-600">Files</p>
      <p className="text-zinc-500">No file wiring yet — fs.list lands in Phase 6.</p>
    </div>
  );
}
