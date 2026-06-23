import Button, { variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

interface SitesEmptyStateProps {
  // Passing onAddSite is optional so pages can hide or wire the CTA based on
  // the active site scope.
  onAddSite?: () => void;
}

const SitesEmptyState = ({ onAddSite }: SitesEmptyStateProps) => (
  <div
    className="flex flex-col items-center justify-center gap-4 rounded-xl border border-border-5 px-6 py-12 text-center"
    data-testid="sites-empty-state"
  >
    <Header title="No sites yet" titleSize="text-heading-300" />
    <p className="max-w-md text-300 text-text-primary-70">Create your first site to organize miners by location.</p>
    <Button
      variant={variants.primary}
      text="Add a site"
      onClick={onAddSite ?? (() => undefined)}
      disabled={!onAddSite}
      testId="sites-empty-state-add"
    />
  </div>
);

export default SitesEmptyState;
