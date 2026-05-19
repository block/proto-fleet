import { Link } from "react-router-dom";

import Button, { variants } from "@/shared/components/Button";

interface BuildingPageHeaderProps {
  label: string;
  buildingId: string;
}

const BuildingPageHeader = ({ label, buildingId }: BuildingPageHeaderProps) => (
  <div className="flex items-start justify-between gap-4">
    <h1 className="text-heading-500 text-text-primary">{label}</h1>
    <div className="flex items-center gap-2">
      <Link to={`/racks?building_id=${buildingId}`} data-testid="building-page-view-racks">
        <Button variant={variants.secondary} text="View racks" onClick={() => undefined} />
      </Link>
      <Link to={`/miners?building_id=${buildingId}`} data-testid="building-page-view-miners">
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
