/* eslint-disable react-refresh/only-export-components -- route scope helpers colocated with tiny route layouts */
import { createContext, type ReactNode, useContext, useEffect, useMemo, useState } from "react";
import { Navigate, Outlet, useParams } from "react-router-dom";

import { buildSiteSlugToId, useSites } from "@/protoFleet/api/sites";
import type { SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import type { ActiveSite } from "@/protoFleet/store/types/activeSite";

const UNASSIGNED_SEGMENT = "unassigned";
const SITE_SLUG_SEGMENT_RE = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;
const NUMERIC_SEGMENT_RE = /^[1-9]\d*$/;
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
  const { listSites } = useSites();
  const [sites, setSites] = useState<SiteWithCounts[] | undefined>(undefined);
  const [loadFailed, setLoadFailed] = useState(false);

  useEffect(() => {
    if (siteScope === UNASSIGNED_SEGMENT || !isSiteSlugSegment(siteScope)) return;
    const controller = new AbortController();
    void listSites({
      signal: controller.signal,
      onSuccess: (rows) => {
        setSites(rows);
        setLoadFailed(false);
      },
      onError: () => {
        setLoadFailed(true);
      },
    });
    return () => controller.abort();
  }, [listSites, siteScope]);

  const slugToId = useMemo(() => buildSiteSlugToId(sites), [sites]);
  const activeSite = activeSiteFromSegment(siteScope, slugToId);

  if (!activeSite) {
    if (isSiteSlugSegment(siteScope) && sites === undefined && !loadFailed) {
      return null;
    }
    return <Navigate to="/" replace />;
  }

  return (
    <SiteScopeProvider value={activeSite}>
      <Outlet />
    </SiteScopeProvider>
  );
};

export const activeSiteFromSegment = (
  segment: string | undefined,
  slugToId?: Map<string, string>,
): ActiveSite | null => {
  if (segment === UNASSIGNED_SEGMENT) return { kind: "unassigned" };
  if (isSiteSlugSegment(segment) && slugToId) {
    const id = slugToId.get(segment);
    if (id) return { kind: "site", id, slug: segment };
  }
  return null;
};

export const segmentFromActiveSite = (activeSite: ActiveSite): string | undefined => {
  switch (activeSite.kind) {
    case "all":
      return undefined;
    case "site":
      return activeSite.slug;
    case "unassigned":
      return UNASSIGNED_SEGMENT;
  }
};

export const isPathScopable = (pathname: string): boolean => {
  return isUnscopedScopablePath(unscopedScopablePath(pathname));
};

export const activeSiteFromScopablePath = (pathname: string, slugToId?: Map<string, string>): ActiveSite | null => {
  const normalized = normalizePathname(pathname);
  if (isUnscopedScopablePath(normalized)) {
    return { kind: "all" };
  }

  const parts = normalized.split("/").filter(Boolean);
  if (parts.length >= 2 && isScopableParts(parts.slice(1))) {
    return activeSiteFromSegment(parts[0], slugToId);
  }

  return null;
};

export const unscopedScopablePath = (pathname: string): string => {
  const normalized = normalizePathname(pathname);
  if (isUnscopedScopablePath(normalized)) {
    return normalized;
  }

  const parts = normalized.split("/").filter(Boolean);
  if (parts.length >= 2 && isScopeSegment(parts[0]) && isScopableParts(parts.slice(1))) {
    return `/${parts.slice(1).join("/")}`;
  }

  return normalized;
};

const isSiteSlugSegment = (segment: string | undefined): segment is string => {
  if (!segment) return false;
  return !NUMERIC_SEGMENT_RE.test(segment) && SITE_SLUG_SEGMENT_RE.test(segment) && !segment.includes("--");
};

const isScopeSegment = (segment: string | undefined): boolean => {
  return segment === UNASSIGNED_SEGMENT || isSiteSlugSegment(segment);
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
  return isScopableParts(parts);
};

const isScopableParts = (parts: string[]): boolean => {
  if (parts.length === 0) return false;
  if (parts[0] === "groups") return parts.length === 1;
  return SCOPABLE_ROOT_SEGMENTS.has(parts[0]);
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
