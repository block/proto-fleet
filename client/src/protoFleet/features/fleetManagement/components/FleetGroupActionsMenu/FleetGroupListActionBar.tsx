import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { type RowAction } from "../RowActionsMenu";
import FleetGroupActionsMenu, { type GroupScope } from "./FleetGroupActionsMenu";
import ActionBar from "@/protoFleet/features/fleetManagement/components/ActionBar";
import { useSetActionBarVisible } from "@/protoFleet/store";
import Button, { sizes, variants } from "@/shared/components/Button";

interface FleetGroupListActionBarProps {
  selectedScopes: GroupScope[];
  kind: "site" | "building" | "rack";
  // Extra actions inserted into the bulk popover — used by lists that
  // need bulk reparent (e.g. Add racks to building / site).
  bulkExtraActions?: RowAction[];
  onClearSelection: () => void;
  onSelectAllVisible: () => void;
  onActionBusyChange?: (busy: boolean) => void;
}

const PLURAL_KIND: Record<FleetGroupListActionBarProps["kind"], string> = {
  site: "sites",
  building: "buildings",
  rack: "racks",
};

const FleetGroupListActionBar = ({
  selectedScopes,
  kind,
  bulkExtraActions,
  onClearSelection,
  onSelectAllVisible,
  onActionBusyChange,
}: FleetGroupListActionBarProps) => {
  const setActionBarVisible = useSetActionBarVisible();
  const [isActionBusy, setIsActionBusy] = useState(false);
  const lastSelectedScopesRef = useRef(selectedScopes);
  if (selectedScopes.length > 0) {
    lastSelectedScopesRef.current = selectedScopes;
  }
  const activeSelectedScopes =
    selectedScopes.length > 0 || !isActionBusy ? selectedScopes : lastSelectedScopesRef.current;
  const selectedIds = useMemo(() => activeSelectedScopes.map((scope) => scope.id.toString()), [activeSelectedScopes]);
  const pluralKind = PLURAL_KIND[kind];
  // Tracks whether the bar is still mounted so a late-arriving onActionComplete
  // can't resurrect the global toaster push-up after the user navigated away.
  const mountedRef = useRef(true);
  // Tracks current selection length so onActionComplete reflects the latest
  // count, not a value captured when the action was dispatched.
  const selectedCountRef = useRef(selectedIds.length);
  selectedCountRef.current = selectedIds.length;

  useEffect(() => {
    setActionBarVisible(selectedIds.length > 0);
  }, [selectedIds.length, setActionBarVisible]);

  useEffect(() => {
    return () => {
      mountedRef.current = false;
      setActionBarVisible(false);
      onActionBusyChange?.(false);
    };
  }, [onActionBusyChange, setActionBarVisible]);

  const handleActionComplete = useCallback(
    (setHidden: (hidden: boolean) => void) => {
      setIsActionBusy(false);
      onActionBusyChange?.(false);
      setHidden(false);
      if (!mountedRef.current) return;
      setActionBarVisible(selectedCountRef.current > 0);
    },
    [onActionBusyChange, setActionBarVisible],
  );

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
          scopes={activeSelectedScopes}
          ariaLabel={`Bulk actions for selected ${pluralKind}`}
          testIdPrefix={`fleet-bulk-${kind}-actions`}
          presentation="bulk"
          bulkExtraActions={bulkExtraActions}
          onActionStart={() => {
            setIsActionBusy(true);
            onActionBusyChange?.(true);
            setHidden(true);
            setActionBarVisible(false);
          }}
          onActionComplete={() => handleActionComplete(setHidden)}
        />
      )}
    />
  );
};

export default FleetGroupListActionBar;
