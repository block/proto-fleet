import PlaceholderBlock from "@/shared/components/PlaceholderBlock";

// Sites tab shell. Real SitesList columns, bulk actions, Add-site CTA, and
// empty-state CTA all land in subsequent PRs. Phase 1a only mounts the
// route + placeholder body so the tab nav can be exercised end-to-end.
const FleetSitesPage = () => (
  <div className="flex flex-col gap-4 p-10 phone:p-6" data-testid="fleet-sites-page">
    <PlaceholderBlock label="Sites list — coming soon" className="h-32" />
  </div>
);

export default FleetSitesPage;
