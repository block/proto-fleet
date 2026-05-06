# RFCs

This directory holds proto-fleet design RFCs: substantive proposals that change architecture, public surface, or the deployment model. Trivial fixes don't need an RFC.

## When to write one

Open an RFC when the change:

- adds or removes a binary, service, or major component
- changes the wire protocol, persistent schema, or auth model
- affects how customers deploy or operate proto-fleet
- needs design review and consensus before implementation begins

## Lifecycle

- `draft`: proposed, under discussion
- `accepted`: agreed; implementation can begin (and may already be in flight in phases)
- `rejected`: closed without acceptance; kept for posterity
- `superseded by RFC NNNN`: replaced by a newer RFC

State lives in the RFC's metadata header (the bullet list at the top of the document) and is updated by PR.

## Numbering

RFCs are numbered sequentially: `0001-`, `0002-`, etc. Pick the next number when opening the PR. Filename: `NNNN-kebab-case-title.md`.

## Format

Copy [`_template.md`](_template.md). Fill it in. Open a PR titled `RFC NNNN: <title>`. Discussion happens in the PR; the merged document is the record of decision.
