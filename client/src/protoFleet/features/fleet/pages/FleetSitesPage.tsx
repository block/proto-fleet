import { useCallback, useEffect, useState } from "react";

import SitesListTable from "../components/SitesListTable";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useSites } from "@/protoFleet/api/sites";
import SiteModals from "@/protoFleet/features/sites/components/SiteModals";
import SitesEmptyState from "@/protoFleet/features/sites/components/SitesEmptyState";
import { useSiteModals } from "@/protoFleet/features/sites/hooks/useSiteModals";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

// Sites tab content for `/fleet/sites`. Mirrors the original
// /settings/sites All Sites table — Add Site CTA, flat list, modals —
// but rows navigate to /sites/:id (J3a) instead of narrowing the
// topbar SitePicker. Phase 1a uses placeholder metrics; the live
// metric columns land with the rest of Phase 1b enrichment.
const FleetSitesPage = () => {
  const { listSites } = useSites();
  const [sites, setSites] = useState<SiteWithCounts[] | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);

  const fetchSites = useCallback(() => {
    const controller = new AbortController();
    void listSites({
      signal: controller.signal,
      onSuccess: (rows) => {
        setSites(rows);
        setError(null);
      },
      onError: (msg) => {
        setError(msg);
        setSites([]);
      },
    });
    return () => controller.abort();
  }, [listSites]);

  useEffect(() => fetchSites(), [fetchSites]);

  const modals = useSiteModals({ refetchSites: fetchSites });

  if (sites === undefined) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6">
        <div className="text-300 text-text-primary-70">Loading…</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="fleet-sites-error">
        <Header title="Couldn't load sites" titleSize="text-heading-200" />
        <p className="text-300 text-text-primary-70">{error}</p>
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          text="Retry"
          onClick={fetchSites}
          testId="fleet-sites-retry"
        />
      </div>
    );
  }

  return (
    <>
      <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="fleet-sites-page">
        {sites.length === 0 ? (
          <SitesEmptyState onAddSite={modals.openCreate} />
        ) : (
          <>
            <div className="flex items-center justify-end">
              <Button
                variant={variants.primary}
                size={sizes.compact}
                text="Add site"
                onClick={modals.openCreate}
                testId="fleet-sites-add"
              />
            </div>
            <SitesListTable sites={sites} />
          </>
        )}
      </div>
      <SiteModals modals={modals} sites={sites} />
    </>
  );
};

export default FleetSitesPage;
