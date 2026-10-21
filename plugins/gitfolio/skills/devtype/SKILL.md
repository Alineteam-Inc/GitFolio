---
name: devtype
description: Explain the user's GitFolio developer type and its five scores, with and without AI, using aline.team's own reasoning for each score. Use when the user asks about their developer type, their scores or how to read or improve them.
allowed-tools: mcp__plugin_gitfolio_gitfolio__status, mcp__plugin_gitfolio_gitfolio__devtype, mcp__plugin_gitfolio_gitfolio__work, mcp__plugin_gitfolio_gitfolio__timeline
---

# Developer type

aline.team works out a developer type from the user's commit history: a type (such as SPRINTER or
KEEPER), a main position, and five scores from 0 to 100 — agility, stability, contribution,
adaptability and consistency — once for the work made without AI and once with it.

## Steps

1. Call `devtype` (`refresh: true` only if the user asks for a fresh one). Not logged in: use the `setup`
   skill. No type yet: aline.team needs commits first (`gitfolio sync`); say so and stop.
2. Explain in the user's language:
   - the type and its description, and the type with AI;
   - each score in a line, strongest and weakest first, with aline.team's reasoning for it (`reasoning`)
     in your own words;
   - where the scores without AI and with it differ, and what that says.
3. If the user asks how to change a score, start from aline.team's reasoning and their own history
   (`work`, `timeline`: commits spread over few days, one language, few tests). Keep it to habits in their
   own work. Do not invent how the scores are computed; say so when you do not know.
