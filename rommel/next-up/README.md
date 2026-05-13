# `next-up/` — the agent backlog

Plans queued for execution, in priority order. An agent picks the top file, moves it to `executing/`, and starts work. Until then, plans here are stable — promoting *into* `next-up/` is the commit moment.

Next stage: `executing/`.
