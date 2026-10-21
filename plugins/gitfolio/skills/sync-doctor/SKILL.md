---
name: sync-doctor
description: Find out why the user's commits or statistics are not on aline.team, using GitFolio's own status, and say what to run. Use when the user says their profile is missing commits, a repository or recent work.
allowed-tools: Bash(gitfolio status *), Bash(gitfolio list), Bash(gitfolio sync --dry-run), Bash(gitfolio config), Bash(gitfolio email), Bash(gitfolio schedule)
---

# Sync doctor

GitFolio collects the user's own pushed commits on this computer and sends them to aline.team. When the
profile looks behind, find which step stopped, then tell the user what to run. Only read: never send,
remove, purge or change settings yourself.

## Steps

1. Read the GitFolio `status` tool (or run `gitfolio status`). For each repository compare `commits`
   with `onAlineTeam`, and look at `waitingForVerifiedEmail`, `syncMode`, `lastSync` and `lastSyncError`.
2. Match what you see:
   - `waitingForVerifiedEmail` set: aline.team takes that repository only after the email is verified —
     `gitfolio email verify <email>`.
   - `syncMode` is `local`: nothing is sent by itself; statistics go when the user runs `gitfolio stats`,
     `stack` or `sync`. `gitfolio config autosync on` sends after every push again.
   - Commits here but fewer on aline.team, no error: run `gitfolio sync --dry-run` to see what would go,
     then suggest `gitfolio sync`.
   - `lastSyncError` set: quote it and explain it.
   - A repository missing from `status`: it is not registered — `gitfolio add <path>`. One with no
     commits: check `gitfolio list` (hooks) and that the commits are pushed and made with one of the
     user's emails (`gitfolio email`).
3. Give the user the commands to run, one per line, and why. Ask before suggesting anything that deletes
   data (`gitfolio remove --purge`).
