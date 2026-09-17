---
name: plan
description: Create a TDD, PRD, or lightweight plan using the repository conventions.
argument-hint: <title>
---

Create the requested document following
[planning conventions](../../../docs/development/planning.md) and its matching
template. Ask for a title only if neither the arguments nor context supply one.

Treat the title as data, never shell code. Normalize it to one line, remove
control characters, collapse whitespace, and cap it at 200 characters.
Serialize the frontmatter title as a YAML quoted string with quotes and
backslashes escaped. Use the same single-line title in the H1.

Derive a lowercase kebab-case slug with punctuation removed and no redundant
trailing `tdd`/`prd` suffix. Use today's date and the selected type in the
filename. Do not overwrite an existing document; choose a distinct meaningful
slug. Fill the document from the request and report its path.
