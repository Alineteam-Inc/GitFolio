---
name: devtype
description: Explain the user's GitFolio developer type and its five scores, with and without AI, and what in their git history the scores reflect. Use when the user asks about their developer type, their scores or how to read them.
allowed-tools: Bash(gitfolio stats *), Bash(gitfolio status *), Bash(gitfolio stack)
---

# Developer type

aline.team works out a developer type from the user's commit history: a type (such as SPRINTER or
KEEPER), a main position, and five scores from 0 to 100 — agility, stability, contribution,
adaptability and consistency — once for the work made without AI and once with it.

## Steps

1. Get the type with the GitFolio `devtype` tool (`refresh: true` only if the user asks for a fresh
   one). Without the tool, run `gitfolio stats` (or `gitfolio stats --refresh`).
2. If GitFolio has no type yet, say that aline.team needs commits first (`gitfolio sync`), and stop.
3. Explain in the user's language:
   - The type and its description as aline.team gives them.
   - Each score in one line, the strongest and weakest first. Compare the scores without AI and with
     it, and say plainly where AI-assisted work changes the picture.
4. If the user asks how to change a score, point to what their history shows (for example from the
   `status` tool: commits spread over few days, one language, one repository) and keep it to habits
   in their own work. Do not invent how aline.team computes the scores; say so when you do not know.
