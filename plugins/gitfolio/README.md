# GitFolio plugin for Claude Code and Codex

Ask your AI coding tool about your own development history: your developer type and scores, how long you
used each technology, and a portfolio written from your pushed commits — through the
[GitFolio CLI](https://github.com/Alineteam-Inc/GitFolio) you already use.

## What it adds

- **MCP server `gitfolio`** (`gitfolio mcp`), with tools that only read:
  - `status` — the repositories GitFolio collects on this computer and what it holds for each
  - `devtype` — your developer type and five scores, without and with AI, from aline.team
  - `stack` — how long you used each language, framework and library, from aline.team
  - `portfolio` — your portfolio in Markdown, as `gitfolio portfolio` writes it
- **Skills**: `portfolio` (a portfolio or resume section, fitted to a job posting if you give one),
  `devtype` (your type and scores explained), `sync-doctor` (why something is missing on aline.team).

## Needs

The GitFolio CLI 0.7.0 or later on your `PATH`, set up and logged in (`gitfolio init`). The plugin does
not install it: see [Install](https://github.com/Alineteam-Inc/GitFolio#install).

## Install

Claude Code:

```sh
/plugin marketplace add Alineteam-Inc/GitFolio
/plugin install gitfolio@alineteam
```

Codex:

```sh
codex plugin marketplace add Alineteam-Inc/GitFolio
codex plugin add gitfolio@alineteam
```

Any other tool that runs MCP servers: add the command `gitfolio mcp`.

## What goes where

- The tools read this computer's GitFolio data and ask aline.team for your developer type and tech stack,
  as `gitfolio stats` and `gitfolio stack` do; in local mode they first run the sync those commands run.
  They never change settings or delete anything.
- What a tool returns — repository names, commit and line counts, languages, your developer type — becomes
  part of the conversation, so your AI tool's provider receives it like anything else you type.
- The plugin itself sends nothing anywhere. What the GitFolio CLI sends to aline.team is listed in its
  [README](https://github.com/Alineteam-Inc/GitFolio#nothing-is-collected-without-your-permission).

Apache-2.0, like GitFolio. GitFolio and aline.team are Alineteam Inc.'s names.
