---
name: tech-career
description: Write the user's tech stack career - how long and where they used each language, framework and library - from GitFolio, for a resume or profile. Use when the user asks for their tech stack, skills section or technology experience.
allowed-tools: mcp__plugin_gitfolio_gitfolio__status, mcp__plugin_gitfolio_gitfolio__stack, mcp__plugin_gitfolio_gitfolio__timeline, mcp__plugin_gitfolio_gitfolio__work, mcp__plugin_gitfolio_gitfolio__devtype
---

# Tech stack career

## Steps

1. **Check.** Call `status`. Not logged in: use the `setup` skill first.
2. **Facts.**
   - `stack`: each language, framework and library with its first and last month, months and number of
     repositories, worked out on this computer as aline.team does.
   - `timeline`: month by month, which languages the user's changes were in — to show when a technology
     was current and how it changed.
   - `work`: per repository, the kinds of work (source, test, build, ci_deploy, database, docs) — to show
     breadth, such as tests, CI/CD or database work. `devtype` gives the main position.
3. **Ask** the user what to stress (the job posting, if any), anything they used that git does not show
   (tools, cloud services), and how they would rate their own level — do not rate it for them.
4. **Write** a short section: grouped (languages, frameworks, libraries, practices), the most relevant
   and recent first, each with its period and months ("Java · 2023-04 ~ 2026-10 · 42 months · 3
   repositories"). Keep the numbers as given; label anything the user added as theirs.
5. Offer to put it into their portfolio (the `portfolio` skill) or save it as Markdown.
