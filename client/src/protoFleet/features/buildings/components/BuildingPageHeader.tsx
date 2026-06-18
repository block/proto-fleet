import { useNavigate } from "react-router-dom";

import { Breadcrumb, type BreadcrumbSegment } from "@/shared/components/Breadcrumb";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

interface BuildingPageHeaderProps {
  label: string;
  buildingId: string;
  onEditBuilding?: () => void;
  breadcrumbSegments?: BreadcrumbSegment[];
}

const BuildingPageHeader = ({ label, buildingId, onEditBuilding, breadcrumbSegments }: BuildingPageHeaderProps) => {
  const navigate = useNavigate();
  return (
    <div className="flex flex-col gap-6">
      {breadcrumbSegments && breadcrumbSegments.length > 0 ? (
        <Breadcrumb segments={breadcrumbSegments} testId="building-page-breadcrumb" />
      ) : null}
      <Header title={label} titleSize="text-heading-300" inline>
        <div className="ml-3 flex items-center gap-3">
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            onClick={() => navigate(`/racks?building=${buildingId}`)}
            testId="building-page-view-racks"
          >
            View racks
          </Button>
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            onClick={() => navigate(`/miners?building=${buildingId}`)}
            testId="building-page-view-miners"
          >
            View miners
          </Button>
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            onClick={onEditBuilding ?? (() => undefined)}
            disabled={!onEditBuilding}
            testId="building-page-edit"
          >
            Edit building
          </Button>
        </div>
      </Header>
    </div>
  );
};

export default BuildingPageHeader;
