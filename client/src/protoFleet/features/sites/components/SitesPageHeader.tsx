import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

interface SitesPageHeaderProps {
  headline: string;
  subheadline: string;
  // Site creation lands in #261. Until then the CTA is inert when no handler
  // is supplied so the scaffold doesn't promise behavior it can't deliver.
  onAddSite?: () => void;
}

const SitesPageHeader = ({ headline, subheadline, onAddSite }: SitesPageHeaderProps) => (
  <div className="flex items-start justify-between gap-6">
    <Header title={headline} titleSize="text-heading-300" description={subheadline} />
    <Button
      variant={variants.primary}
      size={sizes.compact}
      text="Add a site"
      onClick={onAddSite ?? (() => undefined)}
      disabled={!onAddSite}
      testId="sites-page-header-add"
    />
  </div>
);

export default SitesPageHeader;
