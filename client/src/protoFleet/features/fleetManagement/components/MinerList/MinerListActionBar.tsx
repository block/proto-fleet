import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import type { SortConfig } from "@/protoFleet/api/generated/common/v1/sort_pb";
import type { InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import type {
  MinerListFilter,
  MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import ModuleActionsMenu from "@/protoFleet/features/containers/ModuleActionsMenu";
import MinerActionsMenu from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu";
import { useSetActionBarVisible } from "@/protoFleet/store";
import ActionBar, { ActionBarSelectionControls } from "@/shared/components/ActionBar";
import { type SelectionMode } from "@/shared/components/List";

interface MinerListActionBarProps {
  selectedMiners: string[];
  modules?: InventoryItem[];
  onClearSelection?: () => void;
  onSelectAll?: () => void;
  selectAllOnPage?: boolean;
  onSelectNone?: () => void;
  selectionMode: SelectionMode;
  totalCount?: number;
  /** Whether a server-side filter is active — drives the "matching filters" count copy in "all" mode. */
  filtersActive?: boolean;
  currentFilter?: MinerListFilter;
  currentSort?: SortConfig;
  miners?: Record<string, MinerStateSnapshot>;
  minerIds?: string[];
  selectionIncludesUnauthenticatedMiner?: boolean;
  onRefetchMiners?: () => void;
  onWorkerNameUpdated?: (deviceIdentifier: string, workerName: string) => void;
}

const MinerListActionBar = ({
  selectedMiners,
  modules = [],
  onClearSelection,
  onSelectAll,
  selectAllOnPage = false,
  onSelectNone,
  selectionMode,
  totalCount,
  filtersActive,
  currentFilter,
  currentSort,
  miners,
  minerIds,
  selectionIncludesUnauthenticatedMiner,
  onRefetchMiners,
  onWorkerNameUpdated,
}: MinerListActionBarProps) => {
  const setActionBarVisible = useSetActionBarVisible();
  const selectedMinersCountRef = useRef(selectedMiners.length);
  const effectiveSelectionMode =
    selectionMode === "all" && (selectAllOnPage || modules.length > 0) ? "subset" : selectionMode;

  useEffect(() => {
    selectedMinersCountRef.current = selectedMiners.length;
    setActionBarVisible(selectedMiners.length > 0);
  }, [selectedMiners.length, setActionBarVisible]);

  useEffect(() => {
    return () => setActionBarVisible(false);
  }, [setActionBarVisible]);

  const selectionControls =
    onSelectAll || onSelectNone ? (
      <ActionBarSelectionControls
        onSelectAll={onSelectAll}
        onSelectNone={onSelectNone}
        visibleOnly={selectAllOnPage || modules.length > 0}
        selectAllTestId="select-all-miners-button"
        selectNoneTestId="select-none-miners-button"
      />
    ) : undefined;

  // Portal to the body so the bar escapes the app shell's fixed `z-20` scroll
  // container (a stacking context). Otherwise the bulk-actions menu — which opens
  // upward into the header band — is trapped below the `z-40` shell header no
  // matter how high we set the bar's own z-index (#727). z-[45] sits above the
  // header but below the phone/tablet nav drawer (`z-50`) so the drawer still
  // covers the bar when open.
  return createPortal(
    <ActionBar
      className="fixed right-0 bottom-4 left-0 z-[45] tablet:left-16 desktop:left-50"
      selectedItems={selectedMiners}
      selectionMode={effectiveSelectionMode}
      totalCount={totalCount}
      filtersActive={filtersActive}
      onClose={onClearSelection}
      selectionControls={selectionControls}
      renderActions={(setHidden) => {
        const minerIdentifiers = selectedMiners.filter((id) => !id.startsWith("module:"));
        const onActionStart = () => {
          setHidden(true);
          setActionBarVisible(false);
        };
        const onActionComplete = () => {
          setHidden(false);
          setActionBarVisible(selectedMinersCountRef.current > 0);
        };

        return (
          <div className="flex items-center gap-2">
            {modules.length > 0 ? (
              <ModuleActionsMenu
                modules={modules}
                presentation="bulk"
                onRefresh={onRefetchMiners ?? (() => undefined)}
                onActionStart={onActionStart}
                onActionComplete={onActionComplete}
              />
            ) : null}
            {minerIdentifiers.length > 0 ? (
              <MinerActionsMenu
                selectedMiners={minerIdentifiers}
                selectionMode={effectiveSelectionMode}
                totalCount={effectiveSelectionMode === "subset" ? minerIdentifiers.length : totalCount}
                currentFilter={currentFilter}
                currentSort={currentSort}
                miners={miners}
                minerIds={minerIds}
                selectionIncludesUnauthenticatedMiner={selectionIncludesUnauthenticatedMiner}
                onRefetchMiners={onRefetchMiners}
                onWorkerNameUpdated={onWorkerNameUpdated}
                onActionStart={onActionStart}
                onActionComplete={onActionComplete}
              />
            ) : null}
          </div>
        );
      }}
    />,
    document.body,
  );
};

export default MinerListActionBar;
