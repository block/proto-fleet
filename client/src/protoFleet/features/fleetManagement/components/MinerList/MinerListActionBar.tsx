import { useRef } from "react";

import { type MiningSelectionKey, selectedMinerIdentifiers } from "./miningSelection";
import type { SortConfig } from "@/protoFleet/api/generated/common/v1/sort_pb";
import type { InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import type {
  MinerListFilter,
  MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import MiningActionsMenu from "@/protoFleet/features/containers/MiningActionsMenu";
import FleetListActionBar from "@/protoFleet/features/fleetManagement/components/ActionBar/FleetListActionBar";
import useActionBarLifecycle from "@/protoFleet/features/fleetManagement/components/ActionBar/useActionBarLifecycle";
import MinerActionsMenu from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu";
import { FleetAccountAssignment } from "@/protoFleet/features/hostedAccounts/components/FleetAccountAssignment";
import type { AssetReference } from "@/protoFleet/features/hostedAccounts/useFleetAccountInventory";
import { ActionBarSelectionControls } from "@/shared/components/ActionBar";
import { type SelectionMode } from "@/shared/components/List";

interface MinerListActionBarProps {
  selectedKeys: MiningSelectionKey[];
  modules?: InventoryItem[];
  assetReferences?: AssetReference[];
  onAssigned?: () => void;
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
  onClearSelection,
  onSelectAll,
  onSelectNone,
  onRefetchMiners,
  onWorkerNameUpdated,
  onAssigned,
  ...selection
}: MinerListActionBarProps) => {
  const { isActionBusy, onActionStart, onActionComplete } = useActionBarLifecycle(selection.selectedKeys.length);
  // Keep an open confirmation's targets and dispatch scope together through polling and filter changes.
  const selectionRef = useRef(selection);
  if (!isActionBusy) {
    selectionRef.current = selection;
  }
  const {
    selectedKeys,
    assetReferences = [],
    modules = [],
    selectAllOnPage = false,
    selectionMode,
    totalCount,
    filtersActive,
    currentFilter,
    currentSort,
    miners,
    minerIds,
    selectionIncludesUnauthenticatedMiner,
  } = selectionRef.current;
  const minerIdentifiers = selectedMinerIdentifiers(selectedKeys);
  let itemNoun = { singular: "miner", plural: "miners" };
  if (modules.length > 0) {
    itemNoun =
      minerIdentifiers.length > 0
        ? { singular: "mining asset", plural: "mining assets" }
        : { singular: "module", plural: "modules" };
  }
  const effectiveSelectionMode =
    selectionMode === "all" && (selectAllOnPage || modules.length > 0) ? "subset" : selectionMode;

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

  return (
    <FleetListActionBar
      isActionBusy={isActionBusy}
      selectedItems={selectedKeys}
      itemNoun={itemNoun}
      selectionMode={effectiveSelectionMode}
      totalCount={totalCount}
      filtersActive={filtersActive}
      onClose={onClearSelection}
      selectionControls={selectionControls}
    >
      <div className="flex min-w-0 flex-wrap items-center justify-end gap-2">
        <FleetAccountAssignment
          assets={assetReferences}
          exactSelection={effectiveSelectionMode !== "all" ? assetReferences.length === selectedKeys.length : false}
          onActionStart={onActionStart}
          onActionComplete={onActionComplete}
          onAssigned={() => {
            onAssigned?.();
          }}
          renderMenu={
            minerIdentifiers.length > 0
              ? (allocationAction) => (
                  <MinerActionsMenu
                    additionalActions={allocationAction ? [allocationAction] : []}
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
                )
              : undefined
          }
        />
        {modules.length > 0 ? (
          <MiningActionsMenu
            kind="module"
            items={modules}
            presentation="bulk"
            onRefresh={onRefetchMiners}
            onActionStart={onActionStart}
            onActionComplete={onActionComplete}
          />
        ) : null}
      </div>
    </FleetListActionBar>
  );
};

export default MinerListActionBar;
