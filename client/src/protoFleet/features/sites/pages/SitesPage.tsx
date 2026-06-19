import { useCallback, useMemo, useRef, useState } from "react";

import SiteModals from "../components/SiteModals";
import SiteOverviewSection from "../components/SiteOverviewSection";
import SitesEmptyState from "../components/SitesEmptyState";
import SitesPageHeader from "../components/SitesPageHeader";
import { useSiteModals } from "../hooks/useSiteModals";
import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { buildKnownSiteIds, useSites } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import PlaceholderBlock from "@/shared/components/PlaceholderBlock";
import { usePoll } from "@/shared/hooks/usePoll";

// `/sites` operational overview. Phase 1a renders the scaffolding — header,
// per-site sections with placeholder metrics + FPO BuildingCards, and the
// empty-state CTA. Real metric components and the production BuildingCard
// land in #263.
const SitesPage = () => {
  const { listSites } = useSites();
  const { listAllBuildings } = useBuildings();
  const [sites, setSites] = useState<SiteWithCounts[] | undefined>(undefined);
  const [sitesError, setSitesError] = useState<string | null>(null);
  const [buildings, setBuildings] = useState<BuildingWithCounts[] | undefined>(undefined);
  const [buildingsError, setBuildingsError] = useState<string | null>(null);

  // sitesLoaded / buildingsLoaded distinguish initial-load failures from
  // poll failures. On init we want the empty-state path (sites=[]) so the
  // operator sees "no sites yet" / retry CTA. Once we've successfully
  // loaded at least once, poll-error paths must preserve the last-good
  // list so a transient hiccup doesn't blank the screen — they show an
  // inline error toast instead.
  const sitesLoadedRef = useRef(false);
  const buildingsLoadedRef = useRef(false);

  // Track sites + sitesError separately so transient failures (network,
  // PermissionDenied for non-admins) don't collapse into "no sites yet"
  // and mislead the operator into thinking the org has no sites.
  //
  // Returns the inflight promise so usePoll schedules the next tick from
  // response completion (not request start) — otherwise slow responses
  // can overlap and a late older response can clobber newer state.
  const fetchSites = useCallback(
    () =>
      listSites({
        onSuccess: (rows) => {
          setSites(rows);
          setSitesError(null);
          sitesLoadedRef.current = true;
        },
        onError: (msg) => {
          setSitesError(msg);
          // Only clear the list on the *initial* failure so the empty-state
          // / retry surface renders. Once we've shown real data, keep it
          // visible across transient poll failures.
          if (!sitesLoadedRef.current) {
            setSites([]);
          }
        },
      }),
    [listSites],
  );

  // One ListBuildings call at the page level, then we bucket the rows by
  // siteId client-side so each SiteOverviewSection can render synchronously
  // from props. Avoids the N+1 per-section ListBuildings concurrency that
  // the earlier scaffold had. Track buildingsError separately so failures
  // don't collapse every site into "No buildings in this site yet."
  // Promise returned for the same poll-lifecycle reason as fetchSites above.
  const fetchBuildings = useCallback(
    () =>
      listAllBuildings({
        onSuccess: (rows) => {
          setBuildings(rows);
          setBuildingsError(null);
          buildingsLoadedRef.current = true;
        },
        onError: (msg) => {
          setBuildingsError(msg);
          if (!buildingsLoadedRef.current) {
            setBuildings([]);
          }
        },
      }),
    [listAllBuildings],
  );

  // Poll both sites + buildings on the same cadence as the per-card stats
  // (POLL_INTERVAL_MS) so building cards stay in sync when racks are added
  // or removed without forcing a manual refresh. The shared usePoll
  // scheduler dedups concurrent fetches and runs an initial fetch on mount,
  // replacing the earlier one-shot useEffect.
  usePoll({ fetchData: fetchSites, poll: true, pollIntervalMs: POLL_INTERVAL_MS });
  usePoll({ fetchData: fetchBuildings, poll: true, pollIntervalMs: POLL_INTERVAL_MS });

  const knownSiteIds = useMemo(() => (sitesLoadedRef.current ? buildKnownSiteIds(sites) : undefined), [sites]);

  const { activeSite } = useActiveSite({ knownSiteIds });

  const modals = useSiteModals({ refetchSites: fetchSites });

  const buildingsBySite = useMemo(() => {
    const grouped = new Map<string, BuildingWithCounts[]>();
    if (!buildings) return grouped;
    for (const b of buildings) {
      const siteId = b.building?.siteId;
      if (siteId === undefined) continue;
      const key = siteId.toString();
      const existing = grouped.get(key);
      if (existing) existing.push(b);
      else grouped.set(key, [b]);
    }
    return grouped;
  }, [buildings]);

  const visibleSites = useMemo(() => {
    if (!sites) return [];
    if (activeSite.kind === "all") return sites;
    if (activeSite.kind === "site") {
      return sites.filter((s) => (s.site?.id ?? 0n).toString() === activeSite.id);
    }
    // "Unassigned" is handled outside this list — see the dedicated branch
    // below. Return [] here so the "no matches" path isn't triggered.
    return [];
  }, [sites, activeSite]);

  if (sites === undefined) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6">
        <SitesPageHeader headline="Sites" />
        <div className="text-300 text-text-primary-70">Loading…</div>
      </div>
    );
  }

  // Full-page error path: only when we have *no* last-good data to show.
  // Once we've rendered real sites at least once, a transient poll error
  // surfaces as the inline banner inside the main layout below — blanking
  // the screen on every hiccup makes the polled view jittery.
  if (sitesError && !sitesLoadedRef.current) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="sites-page-error">
        <SitesPageHeader headline="Sites" />
        <div
          className="flex flex-col items-start gap-3 rounded-xl border border-border-5 p-6"
          data-testid="sites-page-error-card"
        >
          <Header title="Couldn't load sites" titleSize="text-heading-200" />
          <p className="text-300 text-text-primary-70">{sitesError}</p>
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="Retry"
            onClick={fetchSites}
            testId="sites-page-retry"
          />
        </div>
      </div>
    );
  }

  // "Add a site" only makes sense from the All Sites view; once a specific
  // site is selected the operator is in single-site context and the CTA
  // would be misleading.
  const showAddSite = activeSite.kind === "all";

  return (
    <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="sites-page">
      <SitesPageHeader headline="Sites" onAddSite={showAddSite ? modals.openCreate : undefined} />
      {sitesError ? (
        // Post-init sites failure: keep the last-good list rendered below
        // and surface the failure as an inline retry banner so the
        // operator sees both the data and the issue at once.
        <Callout
          intent="danger"
          prefixIcon={<Alert />}
          title="Couldn't refresh sites"
          subtitle={sitesError}
          buttonText="Retry"
          buttonOnClick={fetchSites}
          testId="sites-page-sites-error"
        />
      ) : null}
      {sites.length === 0 ? (
        <SitesEmptyState onAddSite={modals.openCreate} />
      ) : activeSite.kind === "unassigned" ? (
        // "Unassigned" filters miners, not sites — there is no site-scoped
        // surface to render here. Stand a placeholder in for now so reviewers
        // see the affordance until #273 lands the real miner-filter view.
        <PlaceholderBlock label='"Unassigned" filters miners, not sites. See #273.' className="h-32" />
      ) : visibleSites.length === 0 ? (
        <div className="rounded-xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70">
          No sites match the current selection.
        </div>
      ) : (
        <div className="flex flex-col gap-12">
          {buildingsError ? (
            <Callout
              intent="danger"
              prefixIcon={<Alert />}
              title="Couldn't load buildings"
              subtitle={buildingsError}
              buttonText="Retry"
              buttonOnClick={fetchBuildings}
              testId="sites-page-buildings-error"
            />
          ) : null}
          {visibleSites.map((site) => {
            const siteId = (site.site?.id ?? 0n).toString();
            return (
              <SiteOverviewSection
                key={siteId}
                site={site}
                buildings={buildingsBySite.get(siteId) ?? (buildings === undefined ? undefined : [])}
              />
            );
          })}
        </div>
      )}
      <SiteModals modals={modals} sites={sites} />
    </div>
  );
};

export default SitesPage;
