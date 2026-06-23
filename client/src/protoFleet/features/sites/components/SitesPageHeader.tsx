import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

interface SitesPageHeaderProps {
  headline: string;
  // Optional — /sites suppresses the subheadline so the metric rows below
  // act as the page's primary information density.
  subheadline?: string;
  // When omitted, the "Add a site" button is hidden entirely. Used by /sites
  // to suppress the CTA outside the All Sites selection per master plan §J8.
  onAddSite?: () => void;
}

const SitesPageHeader = ({ headline, subheadline, onAddSite }: SitesPageHeaderProps) => (
  <div className="flex items-start justify-between gap-6">
    <Header title={headline} titleSize="text-heading-300" description={subheadline} />
    {onAddSite ? (
      <Button
        variant={variants.primary}
        size={sizes.compact}
        text="Add a site"
        onClick={onAddSite}
        testId="sites-page-header-add"
      />
    ) : null}
  </div>
);

export default SitesPageHeader;
