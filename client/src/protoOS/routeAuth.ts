import { matchPath } from "react-router-dom";

type SettingsRouteMetadata = {
  path: string;
  requiresAuth?: boolean;
};

export const settingsRouteMetadata = {
  // Authentication settings stay reachable so a locked-out user can change
  // or reset their password without first logging in.
  authentication: { path: "authentication", requiresAuth: false },
  general: { path: "general", requiresAuth: true },
  miningPools: { path: "mining-pools", requiresAuth: true },
  hardware: { path: "hardware", requiresAuth: true },
  cooling: { path: "cooling", requiresAuth: true },
} satisfies Record<string, SettingsRouteMetadata>;

const normalizePathname = (path: string) => {
  if (path === "/") {
    return path;
  }

  return path.replace(/\/+$/, "");
};

export const isAuthRequiredPath = (path: string) => {
  const normalizedPath = normalizePathname(path);

  // Onboarding runs before credentials are established and must stay accessible.
  if (normalizedPath.startsWith("/onboarding")) return false;

  // Respect per-route overrides in settings metadata (e.g. /settings/authentication
  // intentionally stays reachable so the user can change or reset their password).
  for (const route of Object.values(settingsRouteMetadata)) {
    const match = matchPath({ path: `/settings/${route.path}`, caseSensitive: false, end: true }, normalizedPath);
    if (match) return route.requiresAuth === true;
  }

  // Every other route now requires auth because firmware gates all data
  // endpoints — an unauthenticated visit to a data page should prompt login.
  return true;
};
