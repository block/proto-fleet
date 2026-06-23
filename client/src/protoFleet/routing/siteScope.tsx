/* eslint-disable react-refresh/only-export-components -- route scope helpers colocated with tiny route layouts */
import { createContext, type ReactNode, useContext } from "react";
import { Navigate, Outlet, useParams } from "react-router-dom";

import type { ActiveSite } from "@/protoFleet/store/types/activeSite";

const UNASSIGNED_SEGMENT = "unassigned";
const SITE_ID_SEGMENT_RE = /^[1-9]\d*$/;
const SCOPABLE_ROOT_SEGMENTS = new Set(["dashboard", "fleet", "groups", "energy", "activity"]);

const SiteScopeContext = createContext<ActiveSite | null>(null);

export const useRouteSiteScope = (): ActiveSite | null => useContext(SiteScopeContext);

export const SiteScopeProvider = ({ value, children }: { value: ActiveSite; children: ReactNode }) => (
  <SiteScopeContext.Provider value={value}>{children}</SiteScopeContext.Provider>
);

export const AllSitesScopeLayout = () => (
  <SiteScopeProvider value={{ kind: "all" }}>
    <Outlet />
  </SiteScopeProvider>
);

export const SiteScopeLayout = () => {
  const { siteScope } = useParams();
  const activeSite = activeSiteFromSegment(siteScope);

  if (!activeSite) {
    return <Navigate to="/" replace />;
  }

  return (
    <SiteScopeProvider value={activeSite}>
      <Outlet />
    </SiteScopeProvider>
  );
};

export const activeSiteFromSegment = (segment: string | undefined): ActiveSite | null => {
  if (segment === UNASSIGNED_SEGMENT) return { kind: "unassigned" };
  if (segment && SITE_ID_SEGMENT_RE.test(segment)) return { kind: "site", id: segment };
  return null;
};

export const segmentFromActiveSite = (activeSite: ActiveSite): string | undefined => {
  switch (activeSite.kind) {
    case "all":
      return undefined;
    case "site":
      return activeSite.id;
    case "unassigned":
      return UNASSIGNED_SEGMENT;
  }
};

export const isPathScopable = (pathname: string): boolean => {
  return isUnscopedScopablePath(unscopedScopablePath(pathname));
};

export const activeSiteFromScopablePath = (pathname: string): ActiveSite | null => {
  const normalized = normalizePathname(pathname);
  if (isUnscopedScopablePath(normalized)) {
    return { kind: "all" };
  }

  const parts = normalized.split("/").filter(Boolean);
  if (parts.length >= 2 && SCOPABLE_ROOT_SEGMENTS.has(parts[1])) {
    return activeSiteFromSegment(parts[0]);
  }

  return null;
};

export const unscopedScopablePath = (pathname: string): string => {
  const normalized = normalizePathname(pathname);
  if (isUnscopedScopablePath(normalized)) {
    return normalized;
  }

  const parts = normalized.split("/").filter(Boolean);
  if (parts.length >= 2 && activeSiteFromSegment(parts[0]) && SCOPABLE_ROOT_SEGMENTS.has(parts[1])) {
    return `/${parts.slice(1).join("/")}`;
  }

  return normalized;
};

export const scopedPath = (to: string, activeSite: ActiveSite): string => {
  const { pathname, search, hash } = splitPath(to);
  if (!isPathScopable(pathname)) {
    return `${normalizePathname(pathname)}${search}${hash}`;
  }
  const unscoped = unscopedScopablePath(pathname);
  const segment = segmentFromActiveSite(activeSite);
  const scoped = segment ? `/${segment}${unscoped}` : unscoped;
  return `${scoped}${search}${hash}`;
};

export const scopeCurrentOrDashboardPath = (
  pathname: string,
  search: string,
  hash: string,
  activeSite: ActiveSite,
): string => {
  if (isPathScopable(pathname)) {
    return scopedPath(`${unscopedScopablePath(pathname)}${search}${hash}`, activeSite);
  }
  return scopedPath("/dashboard", activeSite);
};

export const appEntryPath = (activeSite: ActiveSite): string => scopedPath("/dashboard", activeSite);

const normalizePathname = (pathname: string): string => {
  if (!pathname.startsWith("/")) return `/${pathname}`;
  return pathname;
};

const isUnscopedScopablePath = (pathname: string): boolean => {
  const parts = normalizePathname(pathname).split("/").filter(Boolean);
  return parts.length > 0 && SCOPABLE_ROOT_SEGMENTS.has(parts[0]);
};

const splitPath = (to: string): { pathname: string; search: string; hash: string } => {
  const hashIndex = to.indexOf("#");
  const beforeHash = hashIndex >= 0 ? to.slice(0, hashIndex) : to;
  const hash = hashIndex >= 0 ? to.slice(hashIndex) : "";
  const searchIndex = beforeHash.indexOf("?");
  const pathname = searchIndex >= 0 ? beforeHash.slice(0, searchIndex) : beforeHash;
  const search = searchIndex >= 0 ? beforeHash.slice(searchIndex) : "";
  return { pathname: pathname || "/", search, hash };
};
