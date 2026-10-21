---
name: portfolio
description: Write the user's portfolio, resume or career description from their git history with GitFolio, by interviewing them briefly - optionally fitted to a job posting. Use when the user asks for a portfolio, a resume or CV, a career description, or a summary of their development work.
allowed-tools: mcp__plugin_gitfolio_gitfolio__status, mcp__plugin_gitfolio_gitfolio__work, mcp__plugin_gitfolio_gitfolio__timeline, mcp__plugin_gitfolio_gitfolio__commits, mcp__plugin_gitfolio_gitfolio__stack, mcp__plugin_gitfolio_gitfolio__devtype, mcp__plugin_gitfolio_gitfolio__ai_settings
---

# Portfolio by interview

GitFolio gives the facts of the user's work: periods, commit and line counts, commit types, kinds of
work, languages, tech stack periods, developer type — and commit messages only where the user allowed
them. What the work was about and what it achieved comes from the user. Ask briefly, write only what you
were given, and never fill a gap with a guess.

## Steps

1. **Check.** Call `status`. Not logged in: use the `setup` skill first.
2. **Purpose.** Ask what it is for: a job posting (ask them to paste it), the format (portfolio page,
   resume bullets, career description per project, English resume) and the language.
3. **Names.** Call `ai_settings`. For each repository shown by its real name, ask: keep the real name,
   use another name (for example "payments backend"), anonymous, or leave it out. Apply the answer with
   `set_ai_exposure`. Company and customer names are the user's call.
4. **Facts.** Call `work` (for the period that matters, if any), `timeline` and `stack`; `devtype` if it
   helps. For repositories that show their messages (`ai_settings`), read `commits` — start with
   `type: feature`, 100 at a time — and group them into a few achievements.
5. **Interview**, per project, at most three questions, each skippable:
   - their role (and team size, if they want);
   - what they built or changed — offer what the facts suggest ("35% features, some database work: what
     did you build?");
   - a measurable result, if any (speed, users, cost, incidents).
   With a job posting, ask first about what it asks for.
6. **Write** in the format and language asked:
   - numbers only as GitFolio gives them or the user says; periods as they are;
   - say what the data shows, not more ("Java for 42 months in 3 repositories", not "Java expert");
   - no names, features or customers the user did not give; tech hidden on their profile (`stack`,
     `isHidden`) stays out;
   - the most relevant work first.
7. **Review and save.** Show the draft, adjust, then offer to save it as Markdown. The user can print a
   Markdown file as a PDF with `gitfolio portfolio --from <file.md>` (Chrome, Edge or Chromium).

Without GitFolio's tools, `gitfolio portfolio /tmp/portfolio.md` writes a starting point from the CLI;
it uses the real repository names, since it is the user's own file.
