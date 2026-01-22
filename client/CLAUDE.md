# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

This repository contains two React applications built with modern web technologies:

- **ProtoOS**: Mining dashboard UI served by the miner's embedded API server (single-miner view)
- **ProtoFleet**: Fleet management UI for managing multiple miners (fleet-wide view)

Both apps share common components and utilities in `src/shared/`.

### Tech Stack

- **React 19** with TypeScript
- **Vite 7** for fast builds and dev server
- **Zustand** for state management with Immer middleware
- **React Router 7** for routing
- **Tailwind CSS 4** for styling
- **Vitest** and Testing Library for testing
- **Storybook 10** for component documentation
- **Recharts** for data visualization
- **Motion** (Framer Motion) for animations

## Development Commands

### Running the Applications

```bash
# Install dependencies
npm install

# Start ProtoOS dev server (default)
npm run dev
npm run dev:protoOS

# Start ProtoFleet dev server
npm run dev:protoFleet

# Access at http://localhost:5173
```


### Building

```bash
# Lint and build both applications
npm run build

# Build individual applications
npm run build:protoOS
npm run build:protoFleet

# Preview production builds
npm run preview:protoOS
npm run preview:protoFleet
```

### Testing

```bash
# Run tests with Vitest
npm test

# Run Storybook for visual component testing
npm run storybook

# Build Storybook
npm run build-storybook
```

### Code Quality

```bash
# Lint code (checks for errors)
npm run lint

# Format code with Prettier
npm run format

# Check formatting without writing
npm run format:check
```

### Running a Single Test

```bash
# Run tests matching a pattern
npx vitest run <test-file-name>

# Run in watch mode for a specific file
npx vitest watch <test-file-name>

# Run tests in a specific directory
npx vitest run src/protoOS/features/kpis
```

## Architecture

### Application Structure

```
src/
├── protoOS/              # Mining dashboard application
│   ├── api/              # API client and generated types
│   ├── components/       # ProtoOS-specific components
│   ├── features/         # Feature modules (auth, kpis, settings, etc.)
│   ├── store/            # Zustand state management
│   ├── router.tsx        # React Router configuration
│   └── index.html        # Entry point
├── protoFleet/           # Fleet management application
│   ├── api/              # Connect-RPC client and Protobuf types
│   ├── components/       # ProtoFleet-specific components
│   ├── features/         # Feature modules (auth, kpis, fleetManagement, etc.)
│   ├── store/            # Zustand state management
│   ├── router.tsx        # React Router configuration
│   └── index.html        # Entry point
└── shared/               # Shared code between applications
    ├── components/       # Reusable UI components
    ├── hooks/            # Custom React hooks
    ├── utils/            # Utility functions
    ├── constants/        # Shared constants
    └── styles/           # Global styles and Tailwind CSS configuration
```

### State Management (ProtoOS)

ProtoOS uses **Zustand with a slice-based architecture** (`useMinerStore`):

- **Hardware Slice**: Static miner hardware info (hashboards, ASICs, PSUs, fans, control board)
- **Telemetry Slice**: Real-time metrics and time-series data
- **UI Slice**: UI state (duration selection, temperature unit, theme preferences)
- **Auth Slice**: Authentication tokens and loading state
- **Miner Status Slice**: Mining status, uptime, errors, and pool information
- **Mining Target Slice**: Power target settings and performance mode
- **Network Info Slice**: Network configuration and connection details
- **System Info Slice**: System-level information and hardware details

Key data types:

- `Measurement`: Single data point with value, units, and formatted display
- `MetricTelemetry`: Combines time series data with latest measurement
- `MetricTimeSeries`: Historical data with aggregates (min/avg/max)

Convenience hooks for accessing store data:

```typescript
import {
  useMinerHashboards,
  useFansTelemetry,
  useDuration,
} from "@/protoOS/store";
```

See `src/protoOS/store/README.md` for comprehensive documentation.

### State Management (ProtoFleet)

ProtoFleet uses **Zustand with a slice-based architecture** (`useFleetStore`):

- **Fleet Slice**: Miner collection, device status counts, filtering, streaming telemetry updates
- **UI Slice**: Theme preferences, temperature unit settings
- **Auth Slice**: Authentication tokens, username, and loading state
- **Onboarding Slice**: Pool configuration and device pairing status

### API Integration

**ProtoOS** - REST API with Swagger/OpenAPI:

- Generated TypeScript client from `proto-rig-api/openapi/MDK-API.json`
- Generated file: `src/protoOS/api/generatedApi.ts`
- Regenerate with: `npm run generate-api-types`
- Application code uses hooks in `src/protoOS/api/hooks/` (not `generatedApi.ts` directly)
- Hooks handle error handling, polling, and automatic store updates

**ProtoFleet** - gRPC-Web with Connect-RPC:

- Generated TypeScript code in `src/protoFleet/api/generated/` from Protobuf definitions
- Service clients in `src/protoFleet/api/clients.ts` created with `@connectrpc/connect`
- Supports server-to-client streaming for real-time telemetry updates
- Custom hooks in `src/protoFleet/api/` (e.g., `useFleet.ts`, `useTelemetryMetrics.ts`)

Key difference: ProtoOS uses REST polling while ProtoFleet uses gRPC streaming for live data.

### Dev Server Proxies

Both apps require proxy configuration to route API requests to backend servers.

Create a `.env` file in the root directory:

**ProtoOS**:

```
PROXY_URL=http://127.0.0.1:8000
```

Routes `/api/v1` requests to the miner API server.

**ProtoFleet**:

```
FLEET_PROXY_URL=http://127.0.0.1:4000
```

Routes `/api-proxy` requests to the fleet server. New API paths must be added to `vite.config.ts`.

### Multi-App Build System

Vite is configured with custom mode-based builds:

- Each app has its own `index.html` entry point in `src/{app}/`
- Build outputs to `dist/{app}/`
- Vite plugins handle HTML file positioning and public directory copying
- Always specify mode when building: `vite build --mode protoOS`

### Path Aliases

Configured in `tsconfig.json` and `vite.config.ts`:

```typescript
"@/*" // Maps to src/* (use this for all absolute imports)
```

Always use `@/` for imports instead of backwards relative paths. Examples:

```typescript
// Good
import { Button } from "@/shared/components/Button";
import { useMinerStore } from "@/protoOS/store";
import { RowItem } from "./RowItem";

// Bad
import { Button } from "../../../shared/components/Button";
import { useMinerStore } from "../../store";
```

### Styling

- **Tailwind CSS 4.x** for utility-first styling
- Global styles in `src/shared/styles/`
- Tailwind theme is defined in `src/shared/styles/theme.css`
- Component-specific styles use Tailwind classes
- Storybook for visual component testing and design system documentation

### Testing

- **Vitest** for unit and integration tests
- **Testing Library** for React component testing
- **Storybook** for visual component testing
- Test setup in `src/tests/setup.ts`
- Tests colocated with components (`.test.tsx` files)

## Feature Modules

Both applications organize features in a modular structure:

**ProtoOS Features**:

- `auth`: Authentication and login
- `kpis`: Key performance indicators (hashrate, efficiency, temperature, etc.)
- `settings`: Miner configuration
- `onboarding`: Initial setup flow
- `firmwareUpdate`: Firmware update management
- `diagnostic`: System diagnostics

**ProtoFleet Features**:

- `auth`: Authentication and fleet login
- `kpis`: Fleet-wide performance metrics
- `fleetManagement`: Miner list and management
- `settings`: Fleet configuration
- `onboarding`: Fleet onboarding flow

## Shared Components

The codebase includes 50+ production-ready shared components in `src/shared/components/`:

**Layout & Structure**: Card, ContentHeader, Divider, BackgroundImage
**Interactive**: Button, ButtonGroup, Dialog, Modal, DurationSelector, Toggle
**Data Display**: Chart (Recharts wrapper), DataNullState, Callout, Chip, StatusBadge
**Forms**: Checkbox, Input, Select, TextArea
**Feedback**: Spinner, ErrorBoundary, Toast
**Specialized**: ComponentStatusModal, EfficiencyValue, MinerStatusIndicator

All shared components:

- Are framework-agnostic (no app-specific logic)
- Have Storybook documentation for visual testing
- Follow consistent styling with Tailwind CSS
- Support both light and dark themes
- Include TypeScript types for props

## Important Implementation Patterns

### API Hook Pattern (ProtoOS)

Hooks follow a clean separation pattern:

1. Fetch data and set local state
2. Separate `useEffect` watches for data changes
3. Update appropriate store slice(s)

Example:

```typescript
useEffect(() => {
  api.getHardware().then((res) => setHardwareData(res));
}, [api]);

useEffect(() => {
  if (!hardwareData) return;
  useMinerStore.getState().hardware.addHashboard(hardwareData);
}, [hardwareData]);
```

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

- Components only used within a specific feature live within that feature's components directory
- Components shared by multiple features, but specific to a single client application (ProtoOS or ProtoFleet) live in their respective `${client}/components` directory (e.g., `src/protoOS/components`)
- Components shared by both clients live in `src/shared/components/` (50+ reusable components)
- Components in `src/shared/components` should be pure components, with consistent output given the same props

Each component typically has:

- Component file (`.tsx`)
- Storybook stories (`.stories.tsx`) - the codebase has 40+ documented component stories
- Tests (`.test.tsx`)
- Index file for clean exports

## Key Files

- `vite.config.ts`: Multi-app build configuration with proxy setup
- `tsconfig.json`: TypeScript configuration with path aliases
- `eslint.config.js`: Linting rules
- `.prettierrc.js`: Code formatting rules
- `package.json`: Dependencies and npm scripts
- `src/protoOS/store/README.md`: Comprehensive state management documentation

## Working with This Codebase

When developing features:

1. Determine if the feature is for ProtoOS, ProtoFleet, or shared
2. Place feature modules in the appropriate `features/` directory
3. Use existing shared components from `src/shared/components/` and `src/{app}/components`
4. Follow the API hook pattern for data fetching
5. Update the appropriate Zustand store slice
6. Create Storybook stories for new components
7. Write tests for new functionality
8. Use the `@/` path alias for imports instead of relative paths
9. Never use multiple backwards relative paths like `../../` - always use absolute paths from the `@/` alias
10. Maintain strict import boundaries:
    - Files in `src/shared/` must never import from `src/protoOS` or `src/protoFleet`
    - Files in `src/protoOS` must never import from `src/protoFleet`, and vice versa
    - This ensures shared code remains truly reusable and applications remain independent
