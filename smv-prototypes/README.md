# Single-Miner View — Delivery, Packaging & Versioning

How do we deliver the single-miner experience, how do we build and version the UI
that powers it, and how do we organize the code so one view can serve every
surface without spawning a firmware × API-version × app matrix?

Prototypes we build for these explorations land under `smv-prototypes/`.

## The constant and the variable

**The constant:** ProtoFleet will always host a single-miner view. When an
operator clicks a miner in the fleet list, we render that miner's detail
experience inside ProtoFleet. That's a given and isn't in question here. The only
sub-decision on the ProtoFleet side is *how it gets the data* — either it proxies
requests through to the live device, or the view is built to read from the fleet
server. Both keep the view inside ProtoFleet.

**The variable — what this doc is actually about:** how is the single-miner
experience delivered *outside* ProtoFleet? When there's no fleet in the picture —
an operator standing in front of a miner, or bringing one up for the first time —
what do they point their browser (or app) at?

- Is it **served off the miner itself** (the firmware hosts a web UI)?
- Is it a **separate application** that connects to a miner (e.g. a desktop app)?
- And whichever we pick, how do we share one view codebase between the in-fleet
  host and the outside-fleet vehicle, so we don't maintain two of everything?

The options below are organized by that outside-ProtoFleet delivery vehicle.

## The four seams every option must answer

This is the bar for each option and any prototype. If an exploration can't answer
all four concretely, it isn't done.

1. **Client delivery** — how the rendered UI reaches the operator's screen outside
   ProtoFleet: served from the miner, or a separate connect-to-a-miner app.
2. **Build artifacts** — what concretely gets built and shipped: which bundles,
   which packages, which versioned artifacts, and who consumes each.
3. **Versioning seam (MDK)** — where differences between miner-API generations are
   absorbed. Today the miner exposes an HTTP/REST API; **MDK v1 and MDK v2 are
   both REST, with different surfaces** (different endpoints and payload shapes for
   the same information). A client that talks to a device must speak whichever
   generation that device runs. Where does that mapping live — build time, runtime
   adapter, or designed away by co-shipping UI with its API?
4. **Code organization** — where the code lives, what the module boundaries are,
   and how the shared UI is packaged so multiple surfaces consume it without
   cumbersome cross-dependencies.

---

## Where we are today (the baseline every option builds from)

Both sides of this already exist; the options are deltas on this baseline, so it's
worth stating precisely.

### The client build (`proto-fleet` repo, `client/`)

- One npm package, `fleet-client`, produces **two apps via Vite build modes**:
  `protoOS` and `protoFleet` (`client/vite.config.ts`, `MODES = ["protoFleet",
  "protoOS"]`). Each mode has its own entry point (`src/protoOS/index.html`,
  `src/protoFleet/index.html`) and its own component tree. `build` runs `vite build
  --mode protoOS && vite build --mode protoFleet`, emitting `dist/protoOS/` and
  `dist/protoFleet/`.
- `src/shared/` holds code both apps use (design primitives, types, utils).
- **ProtoFleet pulls ProtoOS in directly.** The fleet app imports the ProtoOS
  single-miner experience as a component
  (`client/src/protoFleet/components/SingleMinerWrapper/`, importing from
  `@/protoOS/...` behind an `eslint no-restricted-imports` exception). It's one
  package, so this is a path import, not a package dependency.
- When embedded in ProtoFleet, ProtoOS's `/api/v1/*` calls are **reverse-proxied**
  to the live miner (`/api-proxy`, `server/internal/handlers/minerproxy/`).
- **There is no adapter/version layer today.** The proxy forwards to whatever the
  device exposes; nothing maps MDK v1 vs. v2 surfaces to a common shape.

### How the miner firmware ingests ProtoOS today (`miner-firmware` repo)

ProtoOS is **not** built inside firmware, not a submodule, not an npm dep. It's a
**pre-built artifact fetched from `proto-fleet` releases**:

- CI in `proto-fleet` publishes ProtoOS as a GitHub release: a
  `proto-os-*.tar.gz` (containing `dist/protoOS/…`) and a `.ipk` package.
- **Dev/sim:** `tools/fetch-proto-os.sh` downloads the tarball (default tag
  `latest`), extracts to `./protoOS/`, and writes a `.version` marker.
- **Device image:** `distro/distro.yaml` declares `proto-os` as a fetched `.ipk`
  pinned to `tag: latest`, injected into the Yocto rootfs; the UI lands at
  `/var/www`.
- **Serving:** the `miner-api-server` crate (actix-web) serves the static files
  from `--www-path /var/www` on port 80.
- **Version surfacing:** `GET /api/v1/system` reports a `web_dashboard` version
  read from `/etc/web_dashboard_version` (written by the `.ipk`). This is
  **display-only** — there is **no compatibility gate**, and ProtoOS floats on
  `latest`. Nothing ties a given ProtoOS build to a firmware/MDK generation.

So today the coupling between the on-miner UI and the API it renders against is
"whatever `latest` happened to be at image-build time." Every option below is, in
part, a proposal for what should replace that.

---

## Option 1 — Served off the miner; firmware owns the UI

**Outside-ProtoFleet delivery:** the miner's firmware hosts the single-miner UI
directly (as it broadly does today via `miner-api-server` + `/var/www`), but the
UI codebase is **owned and built by the firmware**, shipping inside the firmware
image alongside the API it renders against. One UI codebase covers all proto miner
types.

- **Client delivery** — firmware's embedded web server serves the UI on-device.
  In ProtoFleet, the in-fleet single-miner view is built to read from the **fleet
  server** rather than proxying the device UI, so the two surfaces are genuinely
  independent.
- **Build artifacts** — the miner UI is built as part of the firmware image
  pipeline. ProtoFleet builds its own bundle. If we want visual consistency, a
  shared design-system package (**protoDS**, see deep dive) is consumed by both at
  build time.
- **Versioning seam (MDK)** — **designed away on-device.** Because the UI ships in
  the same firmware release as the API it calls, they're always the same MDK
  generation: no runtime probe, no per-version adapter on the device. The in-fleet
  view sidesteps MDK too, because it reads normalized fleet data rather than the
  raw device API. MDK differences collapse back to the fleet's collector/plugins,
  which already handle them.
- **Code organization** — the miner UI codebase is **owned by the firmware repo**
  (firmware does *not* pull ProtoOS in). `client/shared` is extracted into
  **protoDS**, which the firmware-owned UI **pulls in as a cross-repo dependency**
  for design consistency even though its components differ (see the deep dive for
  how). The ProtoOS-embedded-in-ProtoFleet path is replaced by fleet-native
  components inside `protoFleet/`.

**Why it's attractive:** API↔UI version matching is *free* — they're the same
artifact. No proxy, no probe, no adapter, no version matrix on the device.

**The core tradeoff:** the ProtoFleet single-miner view (fleet-sourced) and the
miner-firmware single-miner view (device-sourced) **will not necessarily have
parity.** They're built from different data at different freshness by different
teams. Accepting that divergence — deciding it's fine for the in-fleet view to
show a fleet-shaped subset while the on-device view shows live device truth — is
the price of this option. Secondary costs: the firmware team now owns a UI build,
and fleet-native parity depends on the fleet actually collecting what the view
needs.

---

## Option 2A — Served off the miner; one shared ProtoOS artifact pulled in

**Outside-ProtoFleet delivery:** the miner still serves the UI, but the UI is a
**single shared ProtoOS artifact** — one `SingleMinerView` over a generic snapshot
data structure, with an **adapter layer** that maps each MDK generation's REST
surface into that snapshot. The *same* artifact is what ProtoFleet embeds and what
firmware pulls into its image. This keeps exactly one view codebase for both the
in-fleet host and the on-miner surface.

- **Client delivery** — one view, two hosts, one artifact: ProtoFleet embeds it
  (reaching the device via an adapter over the proxy), and firmware pulls the same
  artifact to serve on-device.
- **Build artifacts** — ProtoOS is built as a **standalone, versioned artifact**
  (the `proto-os-vX.Y.Z` tarball/`.ipk` the firmware pipeline already knows how to
  consume), plus a library entry that ProtoFleet imports. This is where the open
  question lives (below).
- **Versioning seam (MDK)** — an explicit **adapter layer inside ProtoOS** maps
  MDK v1 and v2 REST surfaces to the common snapshot. **Open question:** do we cut
  a **version-specific artifact per MDK generation** (seam resolved at build time;
  firmware pulls the matching one — deterministic but reintroduces a build matrix),
  or ship **one artifact with all adapters baked in** that selects at runtime (one
  thing to ship; every host carries every adapter)? Resolving this is the primary
  goal of a 2A prototype.
- **Code organization** — ProtoOS becomes its own package (view + snapshot
  contract + adapters), depended on by both ProtoFleet and (as a built artifact)
  firmware, with protoDS underneath it. See the deep dives.

**Why it's attractive:** exactly one view codebase serves both the in-fleet and
on-miner surfaces, and both surfaces *can* reach parity because they render the
same component from the same snapshot. Adding a new MDK generation is a new
adapter, not a new app.

**The core tradeoff:** the version matrix doesn't vanish — it moves into the
artifact/adapter decision, and that decision is load-bearing. We also keep a
device dependency and (for the in-fleet embed) the proxy. Publishing ProtoOS as a
dependency introduces a release-coordination seam between the view and its two
consumers.

---

## Option 2B — A separate desktop app you connect to a miner

**Outside-ProtoFleet delivery:** instead of serving the UI from the miner, ship a
**bespoke Tauri/Electron desktop app** as the connect-to-a-miner surface. Its core
is identical to 2A — one `SingleMinerView` over a generic snapshot with an adapter
layer — but the packaging target is a native app on the operator's machine.

- **Client delivery** — a native desktop app the operator installs. Because it's
  native rather than a browser page, it can talk to the device directly without
  browser CORS/TLS constraints — so no proxy is required for this path. ProtoFleet
  still embeds the same view.
- **Build artifacts** — per-OS desktop binaries (signed, auto-updating) wrapping
  the shared ProtoOS core, plus the ProtoFleet bundle that consumes the same view
  package.
- **Versioning seam (MDK)** — the same adapter layer as 2A, resolved at runtime. A
  native HTTP client makes "one app with all adapters baked in" the natural fit,
  since the browser constraints that complicate direct multi-version device access
  don't apply.
- **Code organization** — the same ProtoOS core package as 2A, plus a thin desktop
  shell module. Firmware is out of the loop for this surface entirely.

**Why it's attractive:** fully decouples the on-site experience from the firmware
release cycle, and the native runtime sidesteps browser limitations on direct
device access.

**The core tradeoff:** a new artifact class to own and operate (signed per-OS
binaries, auto-update, install step on operator machines) — heavier than a URL —
and, like any device-direct path, it still doesn't answer "how do I reach a miner
I can't route to."

**How big is the lift (with Tauri)?** Smaller than it looks, and dominated by ops
rather than UI code — *if 2A already exists*, because the desktop app reuses the
same ProtoOS view + adapters verbatim and just wraps the existing Vite build in a
native shell. Breaking it down:

- **Shell + reuse (small).** Tauri points at the existing web build; the frontend
  is unchanged. Scaffolding the Rust shell is a day or two.
- **Native device access (small–medium, and the actual reason to go native).** The
  one piece of real new code: route the adapter's HTTP calls through Tauri's HTTP
  capability (or a thin Rust command) so we hit the miner directly, bypassing the
  browser CORS/TLS constraints that make in-page direct-to-device access awkward.
- **Connect-to-a-miner UX (small).** Enter/scan IP + credentials, optional
  discovery — a modest surface the browser paths don't need.
- **Packaging pipeline (the long pole — medium, and mostly one-time).** Per-OS
  bundles, macOS code-signing + notarization, and auto-update (Tauri's updater).
  This is a new release/CI pipeline and ongoing per-OS QA, not app code — it's
  where most of the calendar time goes.

**Tauri vs. Electron:** prefer **Tauri**. It uses the OS webview and a Rust core —
much smaller binaries, and Rust aligns with the firmware team's existing
expertise. Electron bundles Chromium (heavier, but more uniform rendering across
OSes); reach for it only if webview rendering differences bite. Net: on top of a
finished 2A, the desktop shell itself is a **days-to-low-weeks** effort; the
signing/notarization/auto-update pipeline is the larger, mostly one-time cost.

---

## Deep dive: code organization in the monorepo

The asks here: take today's code org as the starting point; if we break ProtoOS
and protoDS into their own packages, how would that actually work, would they be
local packages, and what would the DX of working on ProtoFleet look like?

### Today

One package, three source roots with an informal DAG enforced by lint:

```
client/                     # single npm package "fleet-client"
  src/
    shared/                 # primitives, types, utils   (imported by both apps)
    protoOS/                # single-miner app  — own index.html entry
    protoFleet/             # fleet app         — own index.html entry
                            #   imports @/protoOS/* for SingleMinerWrapper
```

Rule (from `AGENTS.md`): `shared/` can't import the apps; the apps can't import
each other — **except** ProtoFleet importing ProtoOS for the embed, which is an
explicit lint exception. So the real dependency graph is already
`shared → protoOS → protoFleet`; it's just expressed as path imports inside one
package, with no independent versioning and no package boundary.

### Proposed: local workspace packages, same DAG made explicit

Convert `client/` into an **npm workspaces** monorepo. Within the monorepo the
packages are consumed **locally** (workspace protocol / symlinks — resolved as
source, not over the network). Whether any package is *also* published for a
consumer outside the repo is a separate, per-option decision — see protoDS in
Option 1 below.

```
client/
  packages/
    protoDS/                # design system: tokens, primitives, snapshot types
    protoOS/                # SingleMinerView + generic snapshot + MDK adapters
  apps/
    protoFleet/             # fleet app; depends on protoDS + protoOS
  package.json              # workspaces: ["packages/*", "apps/*"]
```

Dependency direction, enforced now by real package boundaries instead of a lint
exception:

```
protoDS  ←  protoOS  ←  protoFleet
   ▲___________________________|
   (protoFleet also depends on protoDS directly)
```

- **protoDS** knows nothing about either app. It's the seam that lets the on-miner
  UI and ProtoFleet look identical without sharing app code.
- **protoOS** depends only on protoDS and owns the adapters (the MDK seam).
- **protoFleet** depends on both, and imports `SingleMinerView` from the
  `protoOS` **package** (`import { SingleMinerView } from "protoos"`) instead of a
  `@/protoOS/*` path — the eslint exception disappears.

### How each consumer ingests protoDS

This is the crux of the ask ("versioning without cumbersome cross-dependencies").
The answer differs by consumer, and — importantly — **firmware's relationship to
protoDS is not the same across the options.**

**ProtoFleet ingests protoDS as source, via the workspace (all options).** With
npm workspaces the dependency is a symlink into `packages/protoDS`; Vite and TS
resolve it like local files. Editing protoDS reflects live in the ProtoFleet dev
server (HMR). No publish step, no version bump to iterate. Same for protoOS.

**Firmware in Options 2A / 2B ingests a built artifact, not protoDS.** Here
firmware pulls the **fully-built, self-contained ProtoOS artifact** (the
`proto-os-*.tar.gz` / `.ipk` it already pulls today) in which protoDS is **already
bundled at build time** by Vite. protoDS is an *internal build-time dependency of
protoOS*, invisible to firmware — there's no cross-repo npm graph, and the only
thing firmware pins is one ProtoOS artifact version. Cross-dependency complexity
stays inside the monorepo and never leaks out.

**Firmware in Option 1 ingests protoDS directly — as a real cross-repo
dependency.** This is the case that's genuinely different. In Option 1 firmware
does *not* pull ProtoOS at all; it **owns its own single-miner UI** (which is what
frees ProtoFleet to rewrite its single-miner view against the fleet server). But
that firmware-owned UI still wants **design consistency** with ProtoFleet, so it
pulls **protoDS** in — even though its actual components and layout differ. So
protoDS has to cross the repo boundary here — as **real, versioned React
components** (tokens *and* primitives), so the firmware UI gets genuine design
consistency, not just a color palette. The goal is to make that sharing cheap.
Two distribution mechanisms are on the table.

**A (preferred). Publish protoDS as a content-driven GitHub-release artifact.**
This reuses the exact pattern we already use to share ProtoOS, which makes it the
most likely to actually be adopted — firmware has the tooling and CI wiring for it
today and needs no npm registry or auth. Mechanics, spelled out:

- proto-fleet CI builds protoDS to a self-contained bundle — compiled ESM/CJS
  component output + the CSS/token files + `.d.ts` type declarations — and packages
  it as `protods-vX.Y.Z.tar.gz`, published as a **GitHub release asset**, exactly
  the way `proto-os-*.tar.gz` is published today.
- **Versioned independently, and cut only when it changes.** Unlike today's
  ProtoOS artifact — which inherits the monorepo `vX.Y.Z` tag and is re-cut on
  every proto-fleet release (`release.yml` stamps every asset with `$TAG_NAME`) —
  protoDS carries its **own semver** from its `package.json`, and its build job is
  **path-filtered to `packages/protoDS/**`** so a new artifact is produced *only
  when the design system actually changed*. Firmware therefore sees a new protoDS
  **only on a real design-system change**, not once per proto-fleet release — no
  churn of no-op bumps. This low-churn property is exactly what makes a cross-repo
  dependency tolerable.
- Firmware fetches and extracts the pinned version with a script that mirrors the
  existing `tools/fetch-proto-os.sh` (download by tag → extract to a vendored dir →
  write a `.version` marker), and its UI build imports protoDS from that local path
  (a `file:` dependency or a path alias). Pin = the protoDS version.

The cost is coarser granularity — you pin and vendor the whole bundle, with no
transitive npm resolution — which for a stable design-system layer is an
acceptable trade.

**B (secondary). Publish protoDS as a versioned npm package.** proto-fleet
publishes `protoDS` to a registry at a semver version; firmware's UI build adds it
as a normal dependency and pins it. Cleaner and more granular than the artifact,
and it gets independent versioning *natively* (a package has its own
`package.json` version). Why it's secondary rather than preferred: it asks
proto-fleet to stand up a **publish step it doesn't have today** and firmware to
resolve from a **registry it doesn't use today** — genuinely new surface on both
sides, versus mechanism A which is a straight copy of a pattern already in
production. See the registry analysis for why the registry choice itself is not a
blocker if we do go this way.

### Registry analysis — does publishing protoDS cause a problem?

Checked both repos' actual config; the short answer is **no hard blocker, but the
two repos sit on different footings** worth calling out:

- **proto-fleet is open-source and pulls exclusively from public npm.**
  `client/.npmrc` is literally `registry=https://registry.npmjs.org`, and the repo
  **publishes nothing today**. So the natural home for a published protoDS is
  **public npm** — consistent with the repo's open-source posture and requiring no
  private-registry auth for anyone. (Publishing an internal-only package from an
  otherwise-public repo would be the awkward path, not the easy one.)
- **miner-firmware has no JavaScript/npm toolchain at all today** — no `.npmrc`,
  no `package.json`; it's Rust + Go + Yocto, and pulls ProtoOS purely as a
  GitHub-release artifact. Option 1 is what would *introduce* a JS build to
  firmware (it now owns a React UI). That new build would pull protoDS from
  wherever it's published.

So the registry "issue" is really a footing mismatch, and it resolves cleanly:

- The **preferred** mechanism (A, the content-driven GitHub-release artifact)
  sidesteps registries entirely and leans on firmware's existing GitHub-release
  fetch tooling — the same path already in production for ProtoOS — so there's no
  registry question to answer at all.
- The **secondary** mechanism (B, the npm package) is where the registry choice
  matters, and it still resolves cleanly: publish protoDS to **public npm**, and
  firmware's new UI build pulls it with no auth — no conflict, since public npm is
  already where proto-fleet's deps come from. (Publishing an internal-only package
  from an otherwise-public repo would be the awkward path, not the easy one.)

The one genuinely new responsibility that spans both mechanisms is that **firmware
gains a JS build it doesn't have today** (it now owns a React UI). On the
proto-fleet side, the preferred mechanism (A) is a small extension of the release
pipeline that already emits ProtoOS artifacts — a path-filtered job plus a new
release asset — whereas the secondary mechanism (B) is where proto-fleet would
take on a real *publish-to-registry* step it has never had. Neither is a registry
incompatibility — just new surface to own.

The through-line: **inside the monorepo, packages are source and share live.**
Across the repo boundary, the unit is a *versioned* thing — a built ProtoOS
artifact (2A/2B) or a published/pinned protoDS (Option 1) — and we keep that
versioned surface small and stable so pinning it stays a one-line, low-churn
decision.

### DX ergonomics of working on ProtoFleet with protoOS/protoDS split out

- **One install, one dev command.** `npm install` at the root links all
  workspaces; `npm run dev` (ProtoFleet) still boots one Vite server. Because the
  deps are symlinked source, editing protoDS or protoOS hot-reloads in the
  ProtoFleet dev server — no build/watch of the dependency packages required.
- **TypeScript** uses project references + path mapping so go-to-definition lands
  in `packages/protoDS/src`, not a `.d.ts`. No pre-compilation to see types.
- **Boundaries become mechanical**, not tribal: the DAG is enforced by which
  package is in each `package.json`'s `dependencies`, so an illegal import
  (protoDS → protoOS) fails to resolve instead of needing a lint rule.
- **The main new cost** is build ordering and guarding against cycles — protoDS
  must never import upward. That's the same constraint we enforce today, just made
  structural. Storybook, tests, and lint each move to (or span) the packages;
  that's the migration work, not a steady-state tax.

---

## Deep dive: versioned artifacts and how firmware pulls them

The ask: how would our build system create versioned artifacts, and how would the
firmware pull them in — grounded in how ProtoOS is pulled in today.

### What the build system produces

For Options 1 and 2A the on-miner surface is a **static ProtoOS bundle**; the unit
of shipping is a **semver-tagged, self-contained artifact** (protoDS bundled in):

- ProtoOS builds to `dist/protoOS/` (as today), then CI packages it as
  `proto-os-vX.Y.Z.tar.gz` **and** a `proto-os_X.Y.Z.ipk`, published as a
  `proto-fleet` release. This is exactly the artifact shape firmware already
  consumes — the change is **a real semver tag instead of floating `latest`**, plus
  a small manifest describing which MDK generation(s) the bundle supports.
- ProtoFleet consumes ProtoOS as a workspace **library** at build time (not the
  tarball), so the fleet embed and the on-miner artifact come from the same source
  at the same version.

### How firmware pulls a *pinned* version (vs. today's `latest`)

Today firmware floats: `tools/fetch-proto-os.sh` grabs the `latest` tarball and
`distro/distro.yaml` injects the `proto-os` `.ipk` at `tag: latest`. To get
deterministic API↔UI pairing we pin:

- **Dev/sim:** pass an explicit version to the fetch script (it already accepts
  one — `download-proto-os v0.1.21`) instead of defaulting to `latest`.
- **Device image:** change `distro.yaml`'s `proto-os` entry from `tag: latest` to
  a concrete `vX.Y.Z` (the release-recipe path already generates a
  `miner-web_X.Y.Z.bb` from a concrete tag). Now the image is reproducible and the
  UI version is a deliberate choice, not a build-time accident.
- **Matching guarantee:** because the on-device UI is injected into the same image
  build as the firmware/API, pinning gives a deterministic pairing. For Option 1
  that pairing is implicit (same repo/build). For 2A we additionally get a
  **compat check**: the firmware already surfaces the UI version via
  `GET /api/v1/system` (`/etc/web_dashboard_version`); we extend that to assert the
  bundled ProtoOS supports the firmware's MDK generation, turning today's
  display-only field into an actual gate.

### What the build systems need to do to support this

- **proto-fleet CI:** add semver tagging + release packaging for ProtoOS (tarball
  + `.ipk` + support manifest); build ProtoOS as both a static bundle and a library
  entry; keep ProtoFleet consuming the library.
- **miner-firmware:** pin the ProtoOS version in `distro.yaml` / the fetch script
  instead of `latest`; optionally enforce the MDK-compat check at boot or in
  `/api/v1/system`.
- **Option 2B only:** a separate desktop-release pipeline (per-OS signed binaries +
  auto-update) that wraps the same ProtoOS library; firmware artifacts are
  unaffected.

---

## Comparison across the four seams

| Seam | Option 1 — Firmware owns UI | Option 2A — Shared ProtoOS artifact | Option 2B — Desktop app |
| --- | --- | --- | --- |
| **Client delivery (outside fleet)** | Firmware serves its own UI; in-fleet view reads fleet server. | Same ProtoOS artifact served on-miner and embedded in fleet. | Native app connects to a miner; fleet still embeds the view. |
| **Build artifacts** | Miner UI built in firmware image; ProtoFleet bundle; optional protoDS. | Versioned `proto-os` artifact + library entry, consumed by both hosts. | Per-OS signed binaries wrapping ProtoOS core + ProtoFleet bundle. |
| **Versioning seam (MDK)** | Designed away on-device (UI ships with its API); fleet view sidesteps MDK. | Adapter layer — open Q: per-version artifacts vs. one artifact, adapters baked in. | Adapter layer, runtime-resolved; native client eases baking all adapters in. |
| **Code organization** | Firmware owns its UI + pulls protoDS as a cross-repo dep; `shared`→protoDS; fleet-native components in `protoFleet/`. | protoDS + protoOS + protoFleet as local workspace packages; firmware pulls the built ProtoOS artifact (protoDS bundled in). | Same packages as 2A + thin desktop shell; firmware uninvolved. |
| **Fleet ↔ on-site parity** | Not guaranteed — different data sources, accepted divergence. | Achievable — same view, same snapshot. | Achievable — same view, same snapshot. |

## Open questions

- **Option 1:** how much divergence between the fleet view and the firmware view is
  acceptable, and does fleet-native sourcing cover enough of the experience?
- **Option 2A:** version-specific artifacts vs. one artifact with all adapters —
  which, and what triggers adapter selection at runtime?
- **All options:** do we turn the existing `/api/v1/system` UI-version field into a
  real MDK-compatibility gate, or keep pinning-by-build-reproducibility only?
- **protoDS extraction & layering:** what exactly is in protoDS, and how is it
  layered — platform-neutral tokens as the shareable base vs. React primitives vs.
  snapshot/view types? How much of today's `client/shared` moves vs. stays
  app-local?
- **protoDS cross-repo distribution (Option 1):** preferred path is a
  **content-driven GitHub-release artifact** — protoDS carries its own semver and is
  re-cut only when `packages/protoDS/**` changes, so firmware sees a new version
  only on a real design-system change, reusing the exact pattern that already ships
  ProtoOS. A **public-npm package** is the secondary option (native independent
  versioning, but new publish/registry surface on both repos). Open sub-question:
  is the whole-bundle granularity of the artifact acceptable long-term, or do we
  eventually want npm's transitive resolution?

## What a prototype for each option must demonstrate

- **Option 1:** firmware-owned UI serving itself with no proxy/adapter, plus a
  fleet-native in-fleet view — and a first cut at extracting protoDS.
- **Option 2A:** the single ProtoOS view rendering both fleet-embedded and
  on-miner from one artifact, with a concrete answer on per-version artifacts vs.
  one-artifact-with-adapters, and the workspace package split standing up.
- **Option 2B:** the same view inside a Tauri/Electron shell hitting a device
  directly through the adapter layer, with runtime version detection.
