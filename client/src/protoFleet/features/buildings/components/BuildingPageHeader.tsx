import { Link } from "react-router-dom";

import Button, { variants } from "@/shared/components/Button";

interface BuildingPageHeaderProps {
  label: string;
  buildingId: string;
}

// "View miners" links to /miners with the building URL filter — matches the
// URL key parsed by filterUrlParams.ts (mirroring the existing `group` and
// `rack` singular keys). "View racks" is disabled because the racks page
// filter ships in #274; until then we render the affordance with an
// explanatory title so reviewers see the planned destination.
const BuildingPageHeader = ({ label, buildingId }: BuildingPageHeaderProps) => (
  <div className="flex items-start justify-between gap-4">
    <h1 className="text-heading-500 text-text-primary">{label}</h1>
    <div className="flex items-center gap-2">
      <span title="Rack list building filter is in development — see #274">
        <Button
          variant={variants.secondary}
          text="View racks"
          onClick={() => undefined}
          disabled
          testId="building-page-view-racks"
        />
      </span>
      <Link to={`/miners?building=${buildingId}`} data-testid="building-page-view-miners">
        <Button variant={variants.secondary} text="View miners" onClick={() => undefined} />
      </Link>
      <Button
        variant={variants.primary}
        text="Edit building"
        // Building edit lands in #262; ManageBuildingModal lands in #264.
        onClick={() => undefined}
        disabled
        testId="building-page-edit"
      />
    </div>
  </div>
);

export default BuildingPageHeader;
