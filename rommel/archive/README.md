# `archive/` — shipped or killed

Two kinds of files end up here:

1. Completed work that has been deployed to production — the plan + completion-doc pair gets archived together.
2. Ideas killed early — the `archive` stage doubles as a kill switch (`funnel.promote(..., to: "archive")` is allowed from any stage).

This folder is append-only in spirit. Nothing should ever leave the archive.
