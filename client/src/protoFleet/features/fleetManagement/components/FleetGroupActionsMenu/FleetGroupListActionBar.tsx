import { useEffect, useMemo, useRef } from "react";

import FleetGroupActionsMenu, { type GroupScope } from "./FleetGroupActionsMenu";
import ActionBar from "@/protoFleet/features/fleetManagement/components/ActionBar";
import { useSetActionBarVisible } from "@/protoFleet/store";
import Button, { sizes, variants } from "@/shared/components/Button";

interface FleetGroupListActionBarProps {
  selectedScopes: GroupScope[];
  kind: "site" | "building" | "rack";
  onClearSelection: () => void;
  onSelectAllVisible: () => void;
}

const PLURAL_KIND: Record<FleetGroupListActionBarProps["kind"], string> = {
  site: "sites",
  building: "buildings",
  rack: "racks",
};

const FleetGroupListActionBar = ({
  selectedScopes,
  kind,
  onClearSelection,
  onSelectAllVisible,
}: FleetGroupListActionBarProps) => {
  const setActionBarVisible = useSetActionBarVisible();
  const selectedIds = useMemo(() => selectedScopes.map((scope) => scope.id.toString()), [selectedScopes]);
  const selectedCountRef = useRef(selectedIds.length);
  const pluralKind = PLURAL_KIND[kind];

  useEffect(() => {
    selectedCountRef.current = selectedIds.length;
    setActionBarVisible(selectedIds.length > 0);
  }, [selectedIds.length, setActionBarVisible]);

  useEffect(() => {
    return () => setActionBarVisible(false);
  }, [setActionBarVisible]);

  return (
    <ActionBar
      className="fixed right-0 bottom-4 left-0 z-20 laptop:left-16 desktop:left-50"
      selectedItems={selectedIds}
      selectionMode="subset"
      itemNoun={{ singular: kind, plural: pluralKind }}
      onClose={onClearSelection}
      selectionControls={
        <>
          <Button
            className="py-1"
            size={sizes.textOnly}
            variant={variants.textOnly}
            textColor="text-core-accent-fill"
            textOnlyUnderlineOnHover={false}
            testId={`select-all-visible-${pluralKind}-button`}
            onClick={onSelectAllVisible}
          >
            Select all visible
          </Button>
          <Button
            className="py-1"
            size={sizes.textOnly}
            variant={variants.textOnly}
            textColor="text-core-accent-fill"
            textOnlyUnderlineOnHover={false}
            testId={`select-none-${pluralKind}-button`}
            onClick={onClearSelection}
          >
            Select none
          </Button>
        </>
      }
      renderActions={(setHidden) => (
        <FleetGroupActionsMenu
          scopes={selectedScopes}
          ariaLabel={`Bulk actions for selected ${pluralKind}`}
          testIdPrefix={`fleet-bulk-${kind}-actions`}
          presentation="bulk"
          onActionStart={() => {
            setHidden(true);
            setActionBarVisible(false);
          }}
          onActionComplete={() => {
            setHidden(false);
            setActionBarVisible(selectedCountRef.current > 0);
          }}
        />
      )}
    />
  );
};

export default FleetGroupListActionBar;
