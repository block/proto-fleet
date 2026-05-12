// Route import factories + prefetch tier definitions for protoOS. Lives
// in a router-independent module so consumers (App, layouts) can import
// tiers without creating a cycle through router.tsx — the router
// statically imports those same component files to build the route tree.
//
// To add a route: define the factory const here, add it to the relevant
// tier export below, and add a lazy() wrapper in router.tsx for the
// routerConfig entry. The tier addition isn't lint-enforced — a missed
// entry leaves the chunk un-warmed without breaking the build.

import type { RouteImporter } from "@/shared/utils/prefetchRoutes";

export const importHashrate = () => import("@/protoOS/features/kpis/components/Hashrate");
export const importEfficiency = () => import("@/protoOS/features/kpis/components/Efficiency");
export const importPowerUsage = () => import("@/protoOS/features/kpis/components/PowerUsage");
export const importTemperature = () => import("@/protoOS/features/kpis/components/Temperature");
export const importHashboardTemperature = () => import("@/protoOS/features/diagnostic/components/HashboardTemperature");
export const importDiagnosticView = () =>
  import("@/protoOS/features/diagnostic/components/DiagnosticView/DiagnosticView");
export const importLogs = () => import("@/protoOS/pages/MinerLogs");
export const importOnboarding = () => import("@/protoOS/features/onboarding/components/Onboarding");
export const importOnboardingWelcome = () => import("@/protoOS/features/onboarding/components/Welcome");
export const importOnboardingVerify = () => import("@/protoOS/features/onboarding/components/Verify");
export const importOnboardingNetwork = () => import("@/protoOS/features/onboarding/components/Network");
export const importOnboardingAuthentication = () => import("@/protoOS/features/onboarding/components/Authentication");
export const importOnboardingMiningPool = () => import("@/protoOS/features/onboarding/components/MiningPool");
export const importSettingsAuthentication = () => import("@/protoOS/features/settings/components/Authentication");
export const importSettingsGeneral = () => import("@/protoOS/features/settings/components/General");
export const importSettingsMiningPools = () => import("@/protoOS/features/settings/components/MiningPools");
export const importSettingsHardware = () => import("@/protoOS/features/settings/components/Hardware");
export const importSettingsCooling = () => import("@/protoOS/features/settings/components/Cooling");

// Top-level destinations reachable from the protoOS sidebar plus the
// KPI tab-strip siblings of the default landing route. App.tsx
// triggers this at idle.
export const globalRoutePrefetch: readonly RouteImporter[] = [
  importHashrate,
  importEfficiency,
  importPowerUsage,
  importTemperature,
  importDiagnosticView,
  importLogs,
  importSettingsGeneral,
];

// Settings sub-routes; SettingsContentLayout triggers this on mount so the
// tab strip is warm by the time the user clicks across.
export const settingsRoutePrefetch: readonly RouteImporter[] = [
  importSettingsAuthentication,
  importSettingsMiningPools,
  importSettingsHardware,
  importSettingsCooling,
];

// Section tier for protoFleet — embeds this router under /miners/:id/*.
// SingleMinerWrapper triggers this on mount so KPI tabs, Logs,
// Diagnostics, and per-miner Settings are warm. Composes global +
// settings tiers so new entries flow through automatically.
export const singleMinerRoutePrefetch: readonly RouteImporter[] = [
  ...globalRoutePrefetch,
  importHashboardTemperature,
  ...settingsRoutePrefetch,
];
