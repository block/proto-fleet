import { useNavigate } from "react-router-dom";

import { scopedPath } from "@/protoFleet/routing/siteScope";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
import Breadcrumb, { type BreadcrumbSibling } from "@/shared/components/Breadcrumb";
import Button, { variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

interface BuildingPageHeaderProps {
  label: string;
  buildingId: string;
  siteId?: string;
  siteName?: string;
  buildingSiblings?: BreadcrumbSibling[];
  // Opens ManageBuildingModal. Optional so callers that don't need the
  // editing surface (e.g. error states, loading) can omit it; the button
  // disables when no handler is wired.
  onEditBuilding?: () => void;
}

// Mirrors RackOverviewPage's header: breadcrumb, heading-300 title, and a
// cluster of three secondary buttons. "View miners" and "View
// racks" link to their respective lists with the `building` URL filter — the
// singular key parsed by filterUrlParams.ts (mirroring the existing `group`
// and `rack` singular keys). RacksPage parses the same param to pre-select
// its building filter chip.
const BuildingPageHeader = ({
  label,
  buildingId,
  siteId,
  siteName,
  buildingSiblings,
  onEditBuilding,
}: BuildingPageHeaderProps) => {
  const navigate = useNavigate();
  const activeSite = useFleetStore((state) => state.ui.activeSite);
  const currentSegment = {
    label,
    siblings: buildingSiblings && buildingSiblings.length > 1 ? buildingSiblings : undefined,
  };
  const breadcrumbSegments = siteId
    ? [{ label: "Sites", to: "/fleet/sites" }, { label: siteName ?? "Site", to: `/sites/${siteId}` }, currentSegment]
    : [{ label: "Buildings", to: "/fleet/buildings" }, currentSegment];

  return (
    <div className="flex flex-col gap-3">
      <Breadcrumb segments={breadcrumbSegments} testId="building-page-breadcrumb" />
      <Header title={label} titleSize="text-heading-300" inline>
        <div className="ml-3 flex items-center gap-3">
          <Button
            variant={variants.secondary}
            onClick={() => navigate(scopedPath(`/fleet/racks?building=${buildingId}`, activeSite))}
            testId="building-page-view-racks"
          >
            View racks
          </Button>
          <Button
            variant={variants.secondary}
            onClick={() => navigate(scopedPath(`/fleet/miners?building=${buildingId}`, activeSite))}
            testId="building-page-view-miners"
          >
            View miners
          </Button>
          <Button
            variant={variants.secondary}
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
