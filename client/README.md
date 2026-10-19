# Client

## Overview

This directory contains two React applications and a shared component library:

- **ProtoOS**: Mining dashboard UI served by the miner's embedded API server (single-miner view)
- **ProtoFleet**: Fleet management UI for managing multiple miners (fleet-wide view)
- **Shared**: Common UI components used by both applications

### Tech Stack

- **React 19** with TypeScript
- **Vite 8** for builds and dev server
- **Zustand** for state management with Immer middleware
- **React Router 7** for routing
- **Tailwind CSS 4** for styling
- **Vitest** and Testing Library for testing
- **Storybook 10** for component documentation
- **Recharts** for data visualization
- **Motion** (Framer Motion) for animations

## Directory Layout

```
client
├── .storybook                # Storybook configuration
├── dist                      # Compiled production output
│  ├── protoFleet             # ProtoFleet build output
│  └── protoOS                # ProtoOS build output
├── public                    # Favicon and static assets
├── scripts                   # Development scripts
├── src
│  ├── protoFleet             # Fleet management UI source
│  │  └── index.html          # ProtoFleet entry point
│  ├── protoOS                # Mining dashboard UI source
│  │  └── index.html          # ProtoOS entry point
│  └── shared                 # Shared components, hooks, and utilities
├── eslint.config.js          # Linting rules
├── package.json              # Dependencies and npm scripts
├── postcss.config.js         # PostCSS/Tailwind configuration
├── tsconfig.json             # TypeScript configuration
└── vite.config.ts            # Vite multi-app build configuration
```

## Getting Started

### Install dependencies

```bash
npm install
```

### Start dev server

```bash
# Start ProtoOS dev server
npm run dev:protoOS

# Start ProtoFleet dev server
npm run dev:protoFleet

# Access at http://localhost:5173
```

### Proxy Setup

Both apps require proxy configuration to route API requests to backend servers. Create a `.env` file in this directory:

**ProtoOS**:

```
PROXY_URL=http://127.0.0.1:8000
```

Routes `/api/v1` requests to the miner API server. The proxy URL can point to a locally running miner-api-server, a test node IP, or a mock data API server like [Stoplight](https://stoplight.io/mocks/proto-mining/mdk-api/656299768).

**ProtoFleet**:

```
FLEET_PROXY_URL=http://127.0.0.1:4000
```

Routes `/api-proxy` requests to the fleet server. If you are implementing a new API endpoint, you may need to add the path to `vite.config.ts`.

## Building

```bash
# Build both applications
npm run build

# Build individual applications
npm run build:protoOS
npm run build:protoFleet

# Preview production builds
npm run preview:protoOS
npm run preview:protoFleet
```

### Multi-App Build System

Vite is configured with mode-based builds. Each app has its own `index.html` entry point in `src/{app}/` and builds to `dist/{app}/`. Always specify the mode when building: `vite build --mode protoOS`.

### CT1 on-device standalone

CT1 uses the same `dist/protoOS` build as the standalone rig dashboard. The browser opens the MDK v2 API listener at `http://<controller>:8080/` and probes `/api/v2/system/info` on that origin. The CT1 adapter makes relative `/api/v2/…` requests with the required `X-Frigate-Actor: proto-os` wire header, without a v1 bearer token or login/onboarding gate. No Vite server, minerproxy, CORS setup, or `PROXY_URL` is needed on the device.

The MDK v2 host packaging flow consumes the existing protoOS artifact:

- Host packaging pins the release tag, full commit, tarball asset and SHA-256. Release/nightly publication here does **not** automatically advance that pin.
- The host verifies the release identity, checksum and archive, and owns any installation-time integrity metadata. Controller images consume the packaged bundle; this repository does not install it.
- The MDK v2 host serves the bundle on the same listener as `/api/v2/`. Follow the host's installation and service-restart procedures when updating the dashboard.

The existing artifact workflow supplies `proto-os-${VERSION}.tar.gz` with archive root **`dist/protoOS`**. Preserve `index.html`, `config.js`, `version.txt`, all `assets/` lazy chunks, and optional favicon/fonts. `version.txt` must identify the release tag and full source commit. Do not flatten the archive or include links, unrelated files, or host-generated integrity metadata. `nfpm-proto-os.yaml` and the `.ipk` remain **R2/OpenWRT-only**, not the MDK v2 CT1 deployment path.

To verify a fresh production build against an isolated same-origin CT1 simulator, activate Hermit from the repository root, then run:

```bash
cd client
npx playwright install chromium # once per browser version
npm run test:e2e:ct1
```

This builds protoOS, starts its own `fake-ct1` on port 18081 serving `dist/protoOS`, and checks desktop/mobile boot, tank/module deep-link reloads, no login gate, and scoped tank stop/start with actor-only decoration and untouched neighboring tank state. It refuses to reuse an existing server and restores the touched simulator scope. See [the E2E guide](e2eTests/protoOS/README.md) for focused commands. The simulator preview does not replace the MDK v2 host's artifact verification or an on-hardware deployment check.

For rollout, select a published validated tarball, advance the MDK v2 host's release pin in a separate change, and run its package/image verification. On an authorized controller, check root and tank/module refreshes, reads, and a narrowly scoped stop/start round trip on the management network. This repository's browser smoke does not install a package, update the host's pin, or validate a physical controller.

## Testing

```bash
# Run all tests
npm test

# Run tests matching a pattern
npx vitest run <test-file-name>

# Watch mode for a specific file
npx vitest watch <test-file-name>

# Run tests in a specific directory
npx vitest run src/protoOS/features/kpis
```

## Code Quality

```bash
# Lint code
npm run lint

# Format code with Prettier
npm run format

# Check formatting without writing
npm run format:check

# Run Storybook for visual component testing
npm run storybook
```

## Architecture

### State Management

**ProtoOS** uses Zustand with a slice-based architecture (`useMinerStore`):

- Hardware, Telemetry, UI, Auth, Miner Status, Mining Target, Network Info, System Info slices
- Key data types: `Measurement`, `MetricTelemetry`, `MetricTimeSeries`
- See `src/protoOS/store/README.md` for comprehensive documentation

**ProtoFleet** uses Zustand with a slice-based architecture (`useFleetStore`):

- Fleet, UI, Auth, Onboarding slices
- Fleet slice handles miner collection, device status counts, filtering, and streaming telemetry

### Native container overview

Fleet's Buildings cards and rows open `/containers/:identifier` inside Fleet's
shell, including FleetNode-owned and unreachable controllers. The page reads
collected data; it does not connect to the controller or require an embedded
ProtoOS proxy. `ContainerService.GetContainer` authorizes the current site and
returns the stable Fleet device ID used to load tanks and modules through the
typed inventory API. All cursor pages are fetched before publishing a refresh.

The page polls metadata and inventory every 15 seconds. Transient failures keep
the topology visible with unavailable current readings and a retry; access
denials or removal clear retained data. Navigation cancels outstanding loads.
Tank cards open `/containers/:identifier/tanks/:tankId` using the stable Fleet
tank ID. Module tiles open the native Fleet module detail page. Container performance uses Fleet's telemetry
API and existing performance charts, scoped to the controller and its site.
Container-reported power remains distinct from summed module equipment power.

In the Miners list, CT1 module rows expand into three read-only hashboard slots.
Expanded modules load current board data from Fleet and retain expansion across
polling refreshes. Boards have durable hardware IDs and separate installation
history, so moving or replacing a board does not merge its telemetry with another
board. See [CT1 hashboard inventory and telemetry](../docs/development/ct1-hashboards.md).

`protoFleet/features/containers` owns the page and container presentation;
Fleet's dashboard owns `ChartWidget`. App-neutral primitives and telemetry
formatters remain in `shared`. ProtoOS owns its on-device Systems view, adapters,
and controls. Fan readings and equipment-control states are not collected by
Fleet yet, so this page identifies those gaps and offers no device commands.

### Native tank detail

Container tank cards and tank rows in Racks open the native tank page. It checks
both typed tank identity and container ancestry, inherits the container's read
access, and uses collected Fleet data for directly managed and FleetNode-owned
controllers. The page provides module equipment totals (separate from plant
power), numbered module states, site/container/Buildings breadcrumbs, and a
**View modules** link to the tank-filtered inventory. Partial totals exclude
missing module readings; stale or entirely missing measurements remain unavailable.
Partial metric values include their reporting-module coverage in inventory, tank
KPIs, and container tank cards, independently of module health.

The tank drawing uses outlined module cards with numbering from the bottom left
and module status markers shared with the legend. Isolated modules retain their
state and count under Needs Attention with the same warning marker.
Unknown modules use an outlined marker and, only when present, a small count
annotation below the status legend, including when the controller remains fresh.
The three hashboard outlines remain neutral until hashboard-level Fleet data lands in #163; board states and
drill-downs are not wired yet.

Performance requests a single server-aggregated tank history response using the
shared Fleet duration setting: 1h, 24h, 7d, 30d, 90d, or 1y. SQL averages each
current, non-retired module within a Unix-epoch-aligned bucket, then sums those
module averages. Staggered timestamps and different sample rates cannot dilute
tank totals. Repeated source observations use the latest collection. Responses
contain at most 1000 buckets. Hashrate/power bounds sum per-module minima/maxima;
efficiency bounds use paired power/hashrate extrema and omit undefined bounds. Missing/non-finite module readings are excluded per
metric; only buckets with no readings remain gaps. Efficiency uses paired
hashrate/power readings from the same modules and is weighted by hashrate. Authorization checks both the tank's
owning container and its current site, including during the aggregation query.
The browser polls one bounded response and does no module aggregation. Tank
charts use explicit TH/s, kW, J/TH, and Celsius units without legacy unit guessing.

History availability follows Fleet's retained raw samples and hourly/daily
summaries. Longer windows can contain gaps where collection or retained summaries
are unavailable.

Module clicks from Miners, container tiles, and tank tiles open the native Fleet
page at `/containers/:identifier/tanks/:tankId/modules/:moduleId`. The page checks
the stable module ID and both ancestors; removed, retired, replaced, and
inaccessible modules are never substituted by position. It shows collected module
KPIs, three hashboard slots, and existing scoped mining actions. The header
status pill shares its presentation with ProtoOS and opens a modal with current
board/PSU issue causes. Hashboard cards show an issue icon only when issues are
reported; the info button opens board status details. Fleet and ProtoOS share the
hashboard card layout. Fleet omits the ASIC preview and shows identity in the
info modal. ASIC average uses the normalized temperature reading; ASIC high uses
the optional `maximumAsicTemperatureC` already preserved in collected MDK health.
Missing, non-finite, or stale temperatures render as a dash. No embedded ProtoOS view or live ASIC requests are used.

Board cards distinguish empty, unknown, isolated, and stale evidence. Source
freshness is checked independently for board state and measurements. Module
refresh failures hide current readings and controls; access loss clears the page.
Stored history remains available for stale controllers once Fleet access is
resolved.

The Other hardware section uses a shared PSU card in Fleet and ProtoOS, with input/output
voltage and power plus average/high PSU temperature. Fleet reads existing collected
`psuElectrical`, `psuThermal`, and `psuIdentity` payloads; no additional device polling is
needed. The three boards report one module-owned PSU: Fleet picks the freshest electrical
and temperature snapshots independently, accounting for source time and embedded sample
age, and never sums duplicate PSU readings. Missing or expired metrics show a dash.
The info button opens PSU-only collected issues and available manufacturer metadata;
the issue indicator is non-interactive.

Performance shows module history using the existing paginated Fleet history API. The page fetches all pages for a fixed time window, refreshes once per
minute, and clears failed results. Requests are cancelled on navigation and
bounded to 20 pages; an exceeded limit asks for a shorter range rather than
showing partial history. Charts show 90-second sample averages for 1h/24h, hourly
averages for 7d, and daily averages for longer windows, with explicit gaps and
SI-to-display conversions. Interactive collected, stale, partial, and replaced
states are under **Proto Fleet / Containers / Native module page** in Storybook.

Tank-scoped cooling/equipment
readings are not available in the current Fleet API; the page omits the empty
cooling section. Tank rows, cards, and the native tank page expose scoped mining
start/stop through the tank actions menu. List selection keeps tank IDs separate
from rack IDs; bulk tank actions apply only to the selected tanks.

Module rows in the Miners list and the native module page expose Start mining
and Stop mining. Module selections use stable Fleet module IDs and a separate
Module actions menu; mixed miner/module selections keep their command paths
separate. Confirmation names the selected modules. The server groups the exact
selection by controller, rechecks current org/site permissions and module
identity, and carries the expected identity through direct and Fleet Node
dispatch. Fleet Node module commands require command protocol V5.

The CT1 plugin checks live controller/module identity and sends only the verified
board identifiers to the native StartMiningScoped/StopMiningScoped RPCs. Replacement, retired, stale,
missing, or ambiguous modules fail without broadening the scope. Module and tank
commands share a per-controller pending-command guard. Completion refreshes
inventory; a partially applied or unknown controller result remains unconfirmed,
while successful controllers retain their confirmed counts.

Fleet resolves stable tank IDs to the current controller/site and queues one
command per controller. Admission requires current topology, driver tank-mining
support, and the requested mining permission at every selected tank’s site. A
pending tank command blocks another command for the same controller. Execution
rechecks tank presence and placement, then reads the controller’s live map and
sends one explicit module scope to `/api/v2/mining/scoped/start` or `stop`. Empty
modules are omitted; empty or unavailable tanks fail without a write. Container
mining intent is never changed by this path.

Tank mining uses the same count-only confirmation dialog as miner bulk actions
and closes when confirmed. The action bar returns after command admission,
while a progress toast follows each controller's existing command batch.
Success reports completed tank counts only after all batches finish. Failed
controller batches can contain partial or unknown writes, so they do not count
as confirmed tank failures or offer a retry. Their completion error preserves
counts from successful controllers and asks the operator to check the remaining
tanks. Missing results or interrupted polling also show an unconfirmed completion
error without a retry. As with miner bulk Sleep/Wake, command completion drives
the success toast; inventory readings update separately through normal polling.
Direct controllers use the plugin SDK; FleetNode controllers require command protocol
V4 and updated SDK/plugin binaries. Older nodes reject this action before
sending it, and failed or ambiguous device writes are terminal rather than
automatically retried. Shared equipment readings remain on the
container page.
Storybook's **Proto Fleet / Containers / Native tank page** exercises the actual
page and collected-data hooks with isolated API fixtures.

### CT1 inventory filters

The Miners tab includes containers in **Buildings** and tanks in **Racks**;
the Racks tab also includes containers in **Buildings**. CT1 options are labeled
with their entity type so identically named or numbered entities stay distinct.
Tanks use stable Fleet tank IDs under repeated
`tank` URL keys. Tank labels include the owning container and site, and options
come from all readable tanks across `ListTankOptions` cursor pages. This label-only
catalog avoids module health and telemetry aggregation.
Selections within Buildings (buildings/containers) or Racks (racks/tanks) combine
with OR; the two categories, site scope, and other filters combine with AND.
Unassigned options retain their conventional placement scope and combine with
selected CT1 entities using the same OR rule within each typed category.
When a container or tank is selected, both placement categories independently
restrict rows. A building plus Unassigned Racks then excludes fully unplaced
miners unless Unassigned Buildings is also selected.
Conventional-only miner selections use the native miner filter so the list,
CSV export, authentication counts, and commands share placement semantics.
For example, a building plus Unassigned Racks also includes fully unplaced miners.
Rack and tank IDs stay independent even when their numeric values match.

Containers use controller device IDs under repeated `container` URL keys, independently of
building IDs. Multiple selected containers combine with OR. Site scope, selected
sites, the Racks filter, telemetry, and other filter categories combine with AND;
contradictory scopes return no rows. Existing tank deep links use stable tank IDs
under `tank` and survive unrelated filter changes. The Racks view accepts only
facility ancestors; tank ancestors apply only to mining assets.

Selections survive refresh and saved views, and clearing the Buildings chip
clears both building and container selections and
leaves other filters intact. Container options come from the readable container
catalog with site labels. Filtering and pagination run on the unified inventory
API; selecting a new scope hides previous-scope rows until its response arrives.
Other miner-only filters keep their existing applicability and can therefore
produce no module matches even when a container or tank is selected.
Container-filtered module lists do not expose conventional miner CSV export.
When a container or tank filter is active, miner actions use explicitly selected
rows from the displayed page. Full-result Select all is unavailable because
native miner command filters cannot express those typed placement scopes.

### API Integration

CT1 health uses `ProtoOSStatusModal` with an optional `scope` (`tank` or `module`)
and `componentAddress` (canonical component reference). The scope determines the
aggregate list and the destination for Back, Escape, and Done from component
details; closing the aggregate returns to its opener. Component selection is
in-app state, not a shareable URL.

`scopeHealth.ts` derives membership from topology and combines current board/PSU
health with normalized active errors. Tank indicators use the same summary, so a
PSU-only fault remains visible. Reported health without an error message produces
a navigable observation, not a fabricated active error. The modal polls current
errors and telemetry; resolved errors disappear on refresh. R2 retains its
existing modal flow. Severity presentation and plant alarm grouping are separate
work. Storybook examples live under **Proto OS / Containers / Health drill-down**.

**ProtoOS** — REST API with generated TypeScript client from `proto-rig-api/openapi/MDK-API.json`. Application code uses hooks in `src/protoOS/api/hooks/` which handle error handling, polling, and automatic store updates. Regenerate types with `npm run generate-api-types`.

**ProtoFleet** — gRPC-Web with Connect-RPC. Generated TypeScript code in `src/protoFleet/api/generated/` from Protobuf definitions. Supports server-to-client streaming for real-time telemetry. Custom hooks in `src/protoFleet/api/`.

Container entries in the building list and grid share an actions menu. For directly
paired controllers, it exposes sleep or wake, plus manage power. Opening the menu
reads the controller's current mining status: stopped containers offer wake, active
containers offer sleep, and unknown or transitioning states offer neither. Commands require confirmation and
the corresponding Fleet permission, then issue one MDK v2 request through the
authenticated miner proxy. Sleep/wake changes container mining intent and leaves
isolated boards under individual control. Power limits come from the controller,
with a minimum of 1 W because zero can remove the controller's power limit.
The menu uses the miner menu’s icons and navigation, operations, and settings groups.
View container, the card, and the list row open Fleet's native container details,
including for FleetNode-owned and unreachable controllers. The menu checks proxy
availability on open and disables controller actions when that check fails;
navigation remains available. Power controls wait for a successful status read,
even for operators who only have power-management permission.
Unpair requires `miner:delete` and confirmation, then removes the controller through
Fleet's `DeleteMiners` API. It remains available when the controller proxy is
unreachable, including FleetNode-owned controllers. After success, the row or card
is removed immediately and inventory refreshes in the background. Failed requests
keep the container visible for retry. Single-container unpairing records
`unpair_container` activity with “Unpaired container” and a container scope;
miner and bulk deletion retain their existing activity labels.

The action contract is `proto-rig-api/openapi/MDK-v2-API.json`, checked against
Frigate revision `7a033dbff47e3521d8b87358c1be63bb337ea636`. Host reboot and firmware
update remain unimplemented in that revision's SystemService. Update firmware is
visible but disabled; the CT1 Fleet driver also lacks the firmware capability. Edit
pool and Update worker name are visible but disabled because the CT1 Fleet driver
lacks pool configuration support. Worker-name updates require reapplying pools, and
the authenticated proxy intentionally rejects direct pool writes. These settings
follow the corresponding miner action permissions. Host reboot, cooling, lighting,
alarm controls, streaming logs, module/feed operations, and advanced plant procedures
are not part of this menu. This surface does not change miner bulk selection,
scheduled commands, or curtailment dispatch. Interactive examples are under
**Proto Fleet / Containers / Actions** in Storybook.

### Observability

Client observability lives in `src/shared/observability/` as a vendor-neutral provider
registry. Datadog RUM is the first provider; a provider is a complete no-op unless its
required config is present. When enabled, Datadog RUM captures page/session data, forwards
React render errors (via the shared `ErrorBoundary`), and injects distributed-tracing
headers on same-origin `/api-proxy` calls. This is future-ready client plumbing for
client→server trace correlation; the bundled backend telemetry path does not currently export
correlated server spans to Datadog.

**Configuration** resolves runtime-first, then build-time:

1. `window.__RUNTIME_CONFIG__[KEY]` — rendered into `config.js` by the deployment nginx
   image at container start from `DD_*` environment variables. Operators enable Datadog on
   a prebuilt image by setting these in their `.env` — no rebuild required.
2. `import.meta.env.VITE_<KEY>` — build-time value for local dev and CI (set in `.env`).

Datadog keys (all read via both mechanisms; runtime `DD_*` / build-time `VITE_DD_*`):

| Key                             | Required                                     | Default                                |
| ------------------------------- | -------------------------------------------- | -------------------------------------- |
| `DD_APPLICATION_ID`             | yes                                          | —                                      |
| `DD_CLIENT_TOKEN`               | yes (public RUM token, not a secret API key) | —                                      |
| `DD_SITE`                       | no                                           | `datadoghq.com`                        |
| `DD_SERVICE`                    | no                                           | `proto-fleet-client`                   |
| `DD_ENV`                        | no                                           | build env (`production`/`development`) |
| `DD_RUM_SAMPLE_RATE`            | no                                           | `100`                                  |
| `DD_SESSION_REPLAY_SAMPLE_RATE` | no                                           | `0`                                    |
| `DD_TRACE_SAMPLE_RATE`          | no                                           | `100`                                  |

Both `DD_APPLICATION_ID` and `DD_CLIENT_TOKEN` must be set to enable; otherwise the client
runs unchanged with no SDK side effects.

**Data minimization:** RUM sends to the operator's own Datadog org. Session Replay is off by default
(`DD_SESSION_REPLAY_SAMPLE_RATE=0`) and masks all text/inputs when enabled
(`defaultPrivacyLevel: "mask"`). A `beforeSend` scrubber removes query strings that RUM does
not need for route-level analysis; extend it in `providers/datadog.ts` if the UI later surfaces
sensitive strings in action names or error metadata.

**Adding a provider** (Sentry, PostHog): implement `ObservabilityProvider` under
`src/shared/observability/providers/`, then add one `registerProvider(...)` call in
`src/shared/observability/providers.ts`. The entry point, transport, and error boundary are
provider-agnostic and do not change. End-to-end distributed traces remain follow-up work: the
backend must deliberately associate the incoming context and export server spans to the same
observability backend.

### Import Rules

Use the `@/` path alias for all absolute imports:

```typescript
// Good
import { Button } from "@/shared/components/Button";
import { useMinerStore } from "@/protoOS/store";

// Bad
import { Button } from "../../../shared/components/Button";
```

Strict import boundaries:

- `src/shared/` must never import from `src/protoOS` or `src/protoFleet`
- `src/protoOS` must never import from `src/protoFleet`, and vice versa

### Component Organization

Components follow a feature-based structure:

```
features/
└── kpis/
    ├── components/       # Feature-specific components
    ├── utils/            # Feature utilities
    ├── types.ts          # Feature types
    └── index.ts          # Public exports
```

- Components used within a single feature live in that feature's `components/` directory
- Components shared across features within one app live in `src/{app}/components/`
- Components shared across both apps live in `src/shared/components/`
- Shared components should be pure — consistent output given the same props

### Shared Components

Reusable components in `src/shared/components/` include:

- **Layout**: Card, ContentHeader, Divider, BackgroundImage
- **Interactive**: Button, ButtonGroup, Dialog, Modal, DurationSelector, Toggle
- **Data Display**: Chart, DataNullState, Callout, Chip, StatusBadge
- **Forms**: Checkbox, Input, Select, TextArea
- **Feedback**: Spinner, ErrorBoundary, Toast

All shared components have Storybook stories, support light/dark themes, and include TypeScript prop types.

### Storybook organization

Use these exact top-level names in story metadata `title` values. The sidebar
shows them in this order, with groups sorted alphabetically within each root:

| Root          | Content                                                         |
| ------------- | --------------------------------------------------------------- |
| `Foundation`  | Design tokens, typography, icons, and design guidance           |
| `Shared`      | App-neutral components and interactions from `src/shared/`      |
| `Proto Fleet` | Fleet-owned components and feature flows from `src/protoFleet/` |
| `Proto OS`    | Device-owned components and feature flows from `src/protoOS/`   |

Group app stories by the relevant existing feature, such as
`Proto Fleet/Containers/Overview` or `Proto OS/Containers/Tanks`. A feature using
shared primitives still belongs to its owning app. Avoid new catch-all roots
such as `Components`, separate product roots for containers, or alternative app
spellings. Keep stories beside their implementations; cross-app design guidance
lives in `src/shared/stories/`. Story title changes also change generated story
URLs, so update any links or automation that reference the old IDs.

`Foundation/Design Drift` lives in
[design-drift.stories.tsx](src/shared/stories/design-drift.stories.tsx). It collects
common pitfalls and proposed improvements using shared primitives and tokens;
its Fleet examples illustrate guidance useful to both apps. When adding or
updating a specimen, show the pitfall and the preferred pattern, explain when
the recommendation applies, and verify referenced source locations and component
APIs. Identify historical examples as such once their source is fixed; do not
present them as outstanding defects. Keep only examples that teach a distinct,
recurring lesson, and check both themes when updating their presentation.

## Testing on Hardware

1. Compile the UI: `npm run build`
2. Build the Linux image via GitHub Actions
3. Transfer the image to the control board's SD card
4. Connect the board via ethernet and access the UI at the board's IP address

## Learn More

- [React](https://react.dev/learn)
- [Vite](https://vitejs.dev/guide/)
- [Tailwind CSS](https://tailwindcss.com/docs/utility-first)
- [Recharts](https://release--63da8268a0da9970db6992aa.chromatic.com/?path=/docs/welcome--docs)

### Hosted account workspaces

Hosts manage clients through Settings → Admin → Clients. Fleet navigation has
no client account switcher. Explicitly opened client workspaces show their name
as plain header context. Client users with one accessible account also see its
name as plain text; users with multiple accounts retain an account picker for their
workspaces at `/accounts/:accountId/:section`. Customers and subclients land on
Overview, followed by Mining assets and Pools. Team, Access,
Subclients and Settings use the API’s effective capabilities. `/accounts` lists
accessible workspaces. Hosts retain Fleet navigation and manage Clients under
Settings → Admin. The owner-facing `/accounts` page directly lists managed clients
and offers **Create client**, without a Join an account CTA. It uses host authority,
not private membership in each client. Rows summarize assigned miners and modules
(including subclient allocations), distinct containers represented by those
modules, and live direct client logins. Containers are placement groups, not
additional assigned assets. Login counts exclude invitations, revoked memberships,
deleted users, removed organization affiliations, ancestor grants, and subclient-only
memberships. Membership-only users retain their account
directory and invitation entry point. The root workspace opens Clients. Hosts allocate reporting capacity from Fleet;
the root account has no separate Mining assets tab.

| Persona                    | Primary experience                                                  |
| -------------------------- | ------------------------------------------------------------------- |
| Host operator              | Existing physical fleet operations in Fleet                         |
| Host account administrator | Clients and reporting assignments in Settings → Admin → Clients     |
| Account viewer             | Whole-account metrics and read-only mining assets                   |
| Pool operator              | Account metrics, assets and existing pool configuration tools       |
| Account administrator      | Direct/subclient comparison, team and permitted allocation controls |
| Subclient                  | Its own assigned estate, without another delegation level           |

Overview counts conventional miners and container modules as mining assets.
Controllers are not additional miners. An account total includes resources
allocated to its subclients; administrators with membership-management or
delegation capability can compare the direct remainder and each subclient.
This reporting privilege does not grant access to a child’s private pools,
team, keys or settings. Private child workspace links still require explicit
account authority. Viewers receive no child ownership labels or owner filters.

Hashrate and mining equipment power show current available readings and separate
reporting counts. Efficiency uses only assets reporting both measurements.
Partial/unknown module coverage, unavailable assets and assets outside the current
site policy are labeled. Allocation counts retain unavailable and outside-policy
assets, with their physical details and readings withheld; suspended child
allocations remain in the parent’s totals. Reports describe current assignments,
not historical ownership or plant power.

Mining assets shows only the selected account’s allocation, including its
subclients. Its Name, Model, Status, Hashrate, Efficiency, Power, and Temp columns
use Fleet's labels, widths, metric cells, and module coverage presentation.
Column visibility and order are saved per login and parent account through the
same Manage columns dialog as Fleet. Sorting applies across the inventory with
missing readings last; cursors are bound to the account, user, filters, and sort.
Both tables use 50-row pages and the same selection bar. Account selection stays
limited to the visible page and exposes only assign/recall actions. Model names
belong to miners; a module never inherits its controller's model or network details.
Fresh temperature and reported efficiency follow the existing mining-asset view's
freshness and site-policy redaction. It separates commercial reporting allocation from physical placement.
Hosts select miners/modules in Fleet and choose **Assign to account**; Fleet’s
**Assigned to** column opens the customer’s allocation inventory under the host’s
authority. Clients and Subclients also open these focused inventories without
requiring access to a child’s private workspace. A parent can recall from the
receiving account’s inventory, including a suspended child’s retained allocations.
An account’s own inventory is read-only; administrators can filter by subclient
and open **Manage subclient assets** for parent-authorized recall. An account
cannot surrender its own allocation using its own delegation grant.
Customer administrators open **Assign assets** from Subclients and choose only
resources held directly by their account. Host assignment and customer delegation
use separate pickers from the inventory’s recall controls. Recall returns the selected
allocation to its parent. Whole-customer recall also clears its subclient
allocation. Fleet assignment resolves exact typed leaves with current allocation revisions in
a bounded lookup; missing or ineligible leaves block the entire selection. It does
not expand a global Fleet selection or silently assign a partial batch.
Selection and confirmation are limited to the displayed page;
filtering a container selects its displayed current modules, never its controller
or future/replacement hardware. Stale assignments require a fresh selection and
review. Retired/outside-policy child allocations can be recalled to clear them.
Assignments do not apply pools, change payout destinations, activate customer
firmware control or move equipment. Host operations continue under existing
organization permissions.

These routes reuse the Fleet layout and shared components without mounting host
fleet providers for account-only users. Page-owned RPC clients capture
`X-Fleet-Account-ID` and abort when account or login changes. Permission changes
reset report and selection state; selected-account reporting and assignment RPCs
currently require a Fleet session rather than an account API key.

Host-created clients record host administration as an ancestor-access grant,
separate from the client's team. Creation does not add the host operator as a
member or count host staff as client logins. Host operators with organization
`account:manage` can open the client workspace and invite its first administrator;
live role changes and client ceilings still constrain that access. An explicit
initial administrator is an actual team member. Customer-created subclients keep
the creating customer's initial administrator membership.

Invitations show a portable code once for manual sharing. Recipients select
**Join an account** on the login screen or account list, paste the code, then use
an existing Fleet login or register a new one. Codes stay out of the URL and
are not retained across reloads. Optional links use
`/account-invitation#token=…` at the sender’s current Fleet address; recipients
must be able to reach that address. The page removes the fragment before
previewing the invitation. New API-key secrets are also shown once for manual copying. Saved pool passwords are
never read back; a revision can retain them only for unchanged slot destinations
and usernames. Incoming profiles pin an immutable revision and require the
current login password when changed.

Ancestor grants can be created for the direct parent and updated for existing
ancestors. Creating a subclient’s first grant to the hosting root needs ancestor
metadata that the current API does not expose.

Historical account telemetry, hashboard-level allocation, automatic allocation of
new hardware, customer firmware commands and pool application remain unsupported.
