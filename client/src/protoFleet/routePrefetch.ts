// Route import factories + prefetch tier definitions for the protoFleet
// shell. Lives in a router-independent module so consumers (App,
// SingleMinerWrapper, settings layout) can import tiers without
// creating a cycle through router.tsx — the router statically imports
// those same component files to build the route tree.
//
// To add a route: define the factory const here, add it to the
// relevant tier export below, and add a lazy() wrapper in router.tsx's
// route tree. The tier addition isn't lint-enforced — a missed entry
// leaves the chunk un-warmed without breaking the build.

import { singleMinerRoutePrefetch } from "@/protoOS/routePrefetch"; // eslint-disable-line no-restricted-imports -- Fleet shell embeds the protoOS single-miner experience
import type { RouteImporter } from "@/shared/utils/prefetchRoutes";

export { singleMinerRoutePrefetch };

export const importDashboard = () => import("@/protoFleet/features/dashboard/pages/Dashboard");
export const importMiners = () => import("./features/fleetManagement/components/Fleet");
export const importActivityPage = () => import("@/protoFleet/features/activity/pages/ActivityPage");
export const importEnergyPage = () => import("@/protoFleet/features/energy/pages/EnergyPage");
export const importServerLogsPage = () => import("@/protoFleet/features/serverLogs/pages/ServerLogsPage");
export const importGroupsPage = () => import("@/protoFleet/features/groupManagement/pages/GroupsPage");
export const importGroupOverviewPage = () => import("@/protoFleet/features/groupManagement/pages/GroupOverviewPage");
export const importRacksPage = () => import("@/protoFleet/features/fleetManagement/pages/RacksPage");
export const importRackOverviewPage = () => import("@/protoFleet/features/fleetManagement/pages/RackOverviewPage");
export const importAuth = () => import("@/protoFleet/features/auth/pages/Auth");
export const importUpdatePassword = () => import("@/protoFleet/features/auth/pages/UpdatePassword");
export const importWelcomePage = () => import("@/protoFleet/features/onboarding/components/Welcome");
export const importMinersPage = () => import("@/protoFleet/features/onboarding/components/Miners");
export const importSecurityPage = () => import("@/protoFleet/features/onboarding/components/Security");
export const importOnboardingSettingsPage = () => import("@/protoFleet/features/onboarding/components/Settings");
export const importSettingsLayout = () => import("@/protoFleet/features/settings/components/SettingsLayout");
export const importSettingsGeneral = () => import("@/protoFleet/features/settings/components/General");
export const importSettingsAuth = () => import("@/protoFleet/features/settings/components/Auth");
export const importSettingsMiningPools = () => import("@/protoFleet/features/settings/components/MiningPools");
export const importSettingsTeam = () => import("@/protoFleet/features/settings/components/Team");
export const importSettingsRoles = () => import("@/protoFleet/features/settings/components/Roles");
export const importSettingsFirmware = () => import("@/protoFleet/features/settings/components/Firmware");
export const importSettingsSchedules = () =>
  import("@/protoFleet/features/settings/components/Schedules/SchedulesPage");
export const importSettingsCurtailment = () => import("@/protoFleet/features/settings/components/Curtailment");
export const importSettingsNotifications = () => import("@/protoFleet/features/notifications/pages/Notifications");
export const importSettingsApiKeys = () => import("@/protoFleet/features/settings/components/ApiKeys");
export const importSiteDetailPage = () => import("@/protoFleet/features/sites/pages/SiteDetailPage");
export const importSitesPage = () => import("@/protoFleet/features/sites/pages/SitesPage");
export const importBuildingPage = () => import("@/protoFleet/features/buildings/pages/BuildingPage");
export const importFleetLayout = () => import("@/protoFleet/features/fleetManagement/components/FleetLayout");
export const importFleetBuildingsPage = () => import("@/protoFleet/features/fleetManagement/pages/FleetBuildingsPage");
export const importFleetSitesPage = () => import("@/protoFleet/features/fleetManagement/pages/FleetSitesPage");
export const importFleetDown = () => import("@/protoFleet/components/FleetDown/FleetDown");

// Sidebar destinations + the default settings sub-route. App.tsx
// triggers this at idle so the first nav click has no Suspense flash.
export const globalRoutePrefetch: readonly RouteImporter[] = [
  importDashboard,
  importFleetLayout,
  importMiners,
  importRacksPage,
  importFleetBuildingsPage,
  importFleetSitesPage,
  importGroupsPage,
  importEnergyPage,
  importActivityPage,
  importSettingsLayout,
  importSettingsGeneral,
];

// Settings sub-routes; SettingsLayout triggers this on mount so the rest of
// the tab strip is warm by the time the user clicks across.
export const settingsRoutePrefetch: readonly RouteImporter[] = [
  importSettingsAuth,
  importSettingsMiningPools,
  importSettingsTeam,
  importSettingsRoles,
  importSettingsFirmware,
  importSettingsSchedules,
  importSettingsCurtailment,
  importSettingsNotifications,
  importSettingsApiKeys,
  importServerLogsPage,
];
