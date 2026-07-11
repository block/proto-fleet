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

### API Integration

**ProtoOS** — REST API with generated TypeScript client from `proto-rig-api/openapi/MDK-API.json`. Application code uses hooks in `src/protoOS/api/hooks/` which handle error handling, polling, and automatic store updates. Regenerate types with `npm run generate-api-types`.

**ProtoFleet** — gRPC-Web with Connect-RPC. Generated TypeScript code in `src/protoFleet/api/generated/` from Protobuf definitions. Supports server-to-client streaming for real-time telemetry. Custom hooks in `src/protoFleet/api/`.

### Observability

Client observability lives in `src/shared/observability/` as a vendor-neutral provider
registry. Datadog RUM is the first provider; a provider is a complete no-op unless its
required config is present. When enabled, Datadog RUM captures page/session data, forwards
React render errors (via the shared `ErrorBoundary`), and injects distributed-tracing
headers on same-origin `/api-proxy` calls so a slow RPC can be traced client→server.

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

**Adding a provider** (Sentry, PostHog): implement `ObservabilityProvider` under
`src/shared/observability/providers/`, then add one `registerProvider(...)` call in
`src/shared/observability/providers.ts`. The entry point, transport, and error boundary are
provider-agnostic and do not change. End-to-end distributed traces also require the backend
to accept/propagate the incoming trace headers.

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
