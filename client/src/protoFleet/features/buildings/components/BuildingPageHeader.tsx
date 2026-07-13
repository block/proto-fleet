import { useNavigate } from "react-router-dom";

import { scopedPath } from "@/protoFleet/routing/siteScope";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
import Breadcrumb, { type BreadcrumbSibling } from "@/shared/components/Breadcrumb";
import { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import ResponsiveActionGroup, { type ResponsiveActionButton } from "@/shared/components/ResponsiveActionGroup";

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
  const viewRacksPath = scopedPath(`/fleet/racks?building=${buildingId}`, activeSite);
  const viewMinersPath = scopedPath(`/fleet/miners?building=${buildingId}`, activeSite);
  const headerButtons: ResponsiveActionButton[] = [
    {
      variant: variants.secondary,
      text: "View racks",
      onClick: () => navigate(viewRacksPath),
      testId: "building-page-view-racks",
    },
    {
      variant: variants.secondary,
      text: "View miners",
      onClick: () => navigate(viewMinersPath),
      testId: "building-page-view-miners",
    },
    {
      variant: variants.secondary,
      text: "Edit building",
      onClick: onEditBuilding ?? (() => undefined),
      disabled: !onEditBuilding,
      testId: "building-page-edit",
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <Breadcrumb segments={breadcrumbSegments} testId="building-page-breadcrumb" />
      <Header
        title={label}
        titleSize="truncate text-heading-300"
        inline
        centerButton
        stackButtonsOnPhone={false}
        buttons={headerButtons}
        buttonSize={sizes.compact}
        buttonsWrapperClassName="hidden tablet:block"
        testId="building-page-title"
      >
        <ResponsiveActionGroup
          buttons={headerButtons}
          buttonSize={sizes.compact}
          className="ml-3 shrink-0 tablet:hidden"
          primaryButtonStrategy="last"
          primaryTestIdSuffix="mobile"
          sheetContentTestId="building-page-action-sheet-content"
          sheetTestId="building-page-action-sheet"
          triggerTestId="building-page-more-actions"
        />
      </Header>
    </div>
  );
};

export default BuildingPageHeader;
