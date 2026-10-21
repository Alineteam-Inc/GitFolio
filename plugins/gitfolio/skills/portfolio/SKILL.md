---
name: portfolio
description: Turn the user's git history into a portfolio or resume section with GitFolio, optionally fitted to a job posting. Use when the user asks for a portfolio, a resume or CV section, or a summary of their development work.
allowed-tools: Bash(gitfolio portfolio *), Bash(gitfolio status *), Bash(gitfolio stack), Bash(gitfolio stats *)
---

# Portfolio from git history

GitFolio writes a factual portfolio from the user's own pushed commits: their developer type and scores,
how long they used each technology, and each repository (commits, lines, languages, share made with AI).
Your job is to turn it into the portfolio the user asked for, without adding facts that are not there.

## Steps

1. Get the portfolio with the GitFolio `portfolio` tool. Without the tool, run `gitfolio portfolio`
   with a file name in a temporary folder (for example `gitfolio portfolio /tmp/gitfolio-portfolio.md`)
   and read that file. If GitFolio says the user is not logged in, ask them to run `gitfolio login`
   in a terminal.
2. Before writing, ask which repositories may be named. Repository names can be a company's or a
   customer's, and the user may want them described instead ("a payments backend").
3. Write the portfolio in the user's language, or the language of the job posting if they give one:
   - Keep every number and date as GitFolio gives it. Do not round up, merge periods or guess.
   - Lead with what the posting asks for; leave out what does not help.
   - Describe the work from the languages, periods and repository descriptions. Say what the data
     shows, not what it might mean ("Java for 42 months across 3 repositories", not "Java expert").
   - Tech that GitFolio leaves out was hidden by the user on their aline.team profile: keep it out.
4. Offer to save it as Markdown. The user can print a saved file as a PDF with
   `gitfolio portfolio --from <file.md>` (it needs Chrome, Edge or Chromium).

The data is the user's own; it reaches you only as this conversation.
