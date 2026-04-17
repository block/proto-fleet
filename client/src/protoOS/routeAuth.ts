import { matchPath } from "react-router-dom";

type SettingsRouteMetadata = {
  path: string;
  requiresAuth?: boolean;
};

export const settingsRouteMetadata = {
  authentication: { path: "authentication", requiresAuth: false },
  general: { path: "general", requiresAuth: false },
  miningPools: { path: "mining-pools", requiresAuth: true },
  hardware: { path: "hardware", requiresAuth: false },
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

  return Object.values(settingsRouteMetadata).some(
    (route) =>
      route.requiresAuth === true &&
      matchPath({ path: `/settings/${route.path}`, caseSensitive: false, end: true }, normalizedPath) !== null,
  );
};
