import PlaceholderBlock from "@/shared/components/PlaceholderBlock";

// Buildings tab shell. List columns, filters, bulk actions, list/grid toggle,
// Add-building CTA all land in subsequent PRs. Phase 1a only mounts the
// route + placeholder body so the tab nav can be exercised end-to-end.
const FleetBuildingsPage = () => (
  <div className="flex flex-col gap-4 p-10 phone:p-6" data-testid="fleet-buildings-page">
    <PlaceholderBlock label="Buildings list — coming soon" className="h-32" />
  </div>
);

export default FleetBuildingsPage;
