# Proto contracts

These rules also apply to SDK contracts under `server/sdk/v1/pb/`.

protovalidate runs on requests only. Request `buf.validate` rules must accept
each knob only in the context that consumes it; callers must not be able to
set values that are silently ignored.

Responses carry structural rules only: bounds, presence/pairing, counts
summing, and enum defined-only. Do not encode engine logic (state derivation,
evidence arithmetic, readiness, or per-device criteria) as response CEL;
that duplicates server logic and never executes in production. Say
"the server derives X" in comments, never "validation enforces X".

While a contract is unmerged it carries no history: no `reserved` numbers
or names and no compatibility values; renumber instead.

Use the [generation skill](../.agents/skills/code-generation/SKILL.md) for
source or Buf configuration changes. Commit source and generated output together.
