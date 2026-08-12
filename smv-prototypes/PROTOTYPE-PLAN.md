# SMV Prototyping Plan — drawing the line of work

Two throwaway spikes, each in its own worktree/branch, built to **surface real
unknowns** rather than to implement the options fully. The governing rule for every
work item below: **build exactly enough to convert one unknown into a known, and
deliberately stub or skip everything else.** A prototype "passes" when it produces
a clear signal (works / doesn't / here's the cost) — not when it's feature-complete.

## Worktree & branch layout

| Worktree | Branch | Covers |
| --- | --- | --- |
| A | `proto/opt1-firmware-owned-smv` | Option 1 |
| B | `proto/opt2-protoos-adapter` | Options 2A **and** 2B |

- **Why two worktrees:** both spikes rewrite the `client/` package layout in
  incompatible ways (Option 1 folds ProtoOS into ProtoFleet as a component; 2A/2B
  makes ProtoOS a standalone consumable package). Physically isolating them keeps
  the two reorgs from colliding and keeps each spike disposable.
- **Why 2A + 2B share one worktree:** they're the *same* ProtoOS package with two
  packaging targets. Once the package + adapter layer exists, the firmware artifact
  and the Tauri app are two consumers of it — building both from one branch is the
  point, and proves the "one core, many surfaces" claim directly.

> **Logistics risk to settle first.** Conductor worktrees normally share one
> install / node_modules. Both spikes restructure the client into npm workspaces,
> which needs its own install and fights that shared model. Decide up front: accept
> a dedicated install per spike worktree, or run the reorg in a throwaway clone.
> This is itself a small unknown worth clearing before the real work.

---

## Option 1 spike (worktree A)

### 1. Reorganize the client — protoDS workspace package + ProtoOS → SingleMinerView component

- **Unknown:** does the workspace split actually build and dev cleanly under our
  Vite/TS setup? Do the package boundaries hold without circular-dep hacks? Does
  `npm run dev` still give one server with HMR across package edits?
- **Build (thin slice):** stand up `packages/protoDS` with a *handful* of real
  shared primitives + tokens (not all of `shared/`); wire ProtoFleet to consume it
  via the workspace; move *only the SingleMinerView slice* of ProtoOS into
  ProtoFleet as a component.
- **Cut:** don't migrate all of `shared/`, don't move all of ProtoOS, don't fix
  every import — move the minimum that exercises the dependency graph.
- **Signal:** a protoDS edit hot-reloads inside the ProtoFleet dev server; `tsc`
  and one build pass; no ugly workarounds. If it *needs* workarounds, that's the
  finding.

### 2. Refactor the single-miner view to talk to the fleet server

This is the money item for Option 1, and it goes **further than the round-1
fleet-native lab** — it operates on the *real* SMV component (post-reorg), not a
sandbox.

- **Unknown:** how much parity does fleet-native actually lose? Specifically the
  component-level (ASIC/hashboard) gap, data freshness, and fleet-mediated
  writes/controls — and is the distilled result credible as *the* ProtoFleet
  experience?
- **Build (thin slice):** render the distilled SMV — identity, KPIs, status, a
  hashboard/ASIC representation, one control — entirely from fleet RPCs for a real
  or seeded miner. Where the fleet can't serve something, **show the gap explicitly**
  (empty/reduced) rather than synthesizing it.
- **Cut:** don't build new fleet collection/storage for component data (that's the
  scoping decision the prototype is meant to *inform*, not resolve); stub the write
  as a fleet-mediated dispatch and note the path rather than wiring it live.
- **Signal:** a written, concrete list of exactly what's missing or reduced vs.
  device-truth, plus a judgment on whether fleet-native SMV clears the bar. That
  list is the deliverable.

### 3. Ops — release the protoDS artifact (content-driven, only-when-changed)

- **Unknown:** can we cut an independently-versioned, path-filtered protoDS
  artifact, and can a consumer fetch + pin it?
- **Build (thin slice):** a script (standing in for CI) that builds protoDS →
  `protods-vX.Y.Z.tar.gz`, versioned from its own `package.json` and gated on
  `packages/protoDS/**` changes; plus a `fetch-proto-os.sh`-style fetch script a
  **stub consumer dir** runs to pin and import it.
- **Cut:** don't touch real miner-firmware; a throwaway consumer proves the
  mechanics. Skip the npm-publish path entirely.
- **Signal:** two runs — one where protoDS changed (new artifact, bumped version)
  and one where it didn't (no new artifact) — demonstrating the only-when-changed
  property end to end.

---

## Option 2A / 2B spike (worktree B)

### 1. Reorganize the client into a full npm workspace

- **Unknown:** ProtoOS as a real **dual-output package** — a library entry that
  ProtoFleet embeds *and* a standalone static bundle for firmware/Tauri — building
  cleanly from one package. Plus the same workspace-DX risk as Option 1.
- **Build (thin slice):** `packages/protoDS`, `packages/protoOS` (exporting
  `SingleMinerView` as a lib **and** carrying its own `index.html` standalone
  entry), `apps/protoFleet` importing the package.
- **Cut:** minimal real content; move only what the two build outputs need.
- **Signal:** `protoOS` emits both a library and a standalone bundle; ProtoFleet
  consumes the lib; dev HMR works.

### 2. Generic view + adapter layer — reads, writes, **and auth** (the core unknown)

The highest-value, highest-risk item in either spike. Round 1 mostly pressed
reads; the seam's real test is writes and auth.

- **Unknown:** does one generic snapshot + adapter seam genuinely absorb MDK v1 vs.
  v2 REST-surface differences across **reads, writes, and auth**? Auth token flows
  and write semantics are where adapters usually crack.
- **Build (thin slice):** the generic `SingleMinerView` over the snapshot; **two
  adapters (v1, v2)** against the existing fake rigs, each implementing (a) one read
  whose shape differs between v1 and v2, (b) one write/control, and (c) the
  auth/login flow.
- **Cut:** not every endpoint — one representative read + one write + auth per
  adapter is enough to prove or break the seam. No third adapter.
- **Signal:** the same view drives an authenticated read *and* a write through both
  adapters unchanged; a written note on where the snapshot/seam had to bend
  (especially auth and writes). This directly informs the *per-version-artifact vs.
  one-artifact-with-adapters* decision.

### 3. In-fleet embed via adapter + proxy

- **Unknown:** does the adapter layer compose with the existing `minerproxy`
  (which already handles device auth/token caching + TLS)? Where does auth live —
  adapter or proxy?
- **Build (thin slice):** mount the generic view in ProtoFleet's
  `SingleMinerWrapper` path and route one adapter's fetch through `/api-proxy` to a
  rig.
- **Cut:** don't re-plumb minerproxy — use it as-is; one read path suffices.
- **Signal:** the in-fleet view renders through adapter + proxy, with a clear note
  on the auth division of labor between adapter and proxy.

### 4. Ops — package ProtoOS (view + probe + adapters) into a firmware artifact

- **Unknown:** bundling the probe + *all* adapters into one standalone static
  artifact — does runtime version detection work when the bundle is served on its
  own, and is the artifact size sane?
- **Build (thin slice):** build `packages/protoOS` standalone → a proto-os-style
  tarball; serve it statically pointed at the fake rigs; confirm the probe selects
  the right adapter at runtime.
- **Cut:** no `.ipk` / Yocto path, no pinning into real firmware — a served static
  bundle proves it.
- **Signal:** one artifact, served standalone, probes and renders both a v1 and a
  v2 rig. This answers the "one artifact with adapters baked in" feasibility
  question concretely.

### 5. Ops — basic Tauri app: connect-to-IP → ProtoOS

- **Unknown:** does the native shell let us hit a device directly through the
  adapter, bypassing the browser CORS/TLS constraints? This is the *entire* reason
  2B exists.
- **Build (thin slice):** a minimal Tauri shell wrapping the ProtoOS bundle, a
  connect-to-IP form, native HTTP to a rig, render.
- **Cut:** **no** signing, notarization, auto-update, or multi-OS builds — one
  dev-built binary on the dev's own OS. Cutting all the ops-heavy packaging is the
  point; keep only the "does native device access work" core.
- **Signal:** the Tauri app hits a rig at an IP that a browser page *couldn't*
  (CORS), and renders the view. Cheaply confirms or kills the native-access premise.

---

## Sequencing

Both worktrees start with the **client reorg** — it gates everything else. Then:

- **Worktree A:** reorg → fleet-native SMV refactor (the parity signal) → DS
  artifact ops (independent, can run in parallel).
- **Worktree B:** reorg → **adapter layer** (do this before ops packaging; it's the
  make-or-break unknown) → in-fleet embed → firmware artifact → Tauri last (it
  depends on the ProtoOS standalone bundle from item 4).

## Shared non-goals (both spikes)

- No new fleet component-data collection/storage — that's a scoping decision the
  Option 1 spike *informs*, not builds.
- No production release pipelines — signing, `.ipk`/Yocto, npm publish are all
  stubbed or stood in for by scripts.
- No exhaustive adapter/endpoint coverage — representative slices only.
- No design polish — these are disposable spikes.
- Don't merge to `main`. The output of each spike is **findings + a recommendation**
  (and possibly a curated subset promoted later), not shippable code.

## What we should be able to answer when both spikes are done

| Question the direction hinges on | Answered by |
| --- | --- |
| How big is the fleet-native parity gap, and is the SMV credible without it? | A · fleet-native refactor |
| Does the adapter seam hold for writes + auth, not just reads? | B · adapter layer |
| Is "one artifact, all adapters baked in" viable, or do we need per-version artifacts? | B · firmware-artifact ops |
| Does a native shell actually buy us direct device access? | B · Tauri app |
| What does the workspace split cost day-to-day (DX, build, boundaries)? | A **and** B · client reorg |
| Can protoDS ship as an independently-versioned, only-when-changed artifact? | A · DS artifact ops |
