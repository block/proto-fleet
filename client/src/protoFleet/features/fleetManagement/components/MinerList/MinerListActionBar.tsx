import { useRef } from "react";

import { type MiningSelectionKey, selectedMinerIdentifiers } from "./miningSelection";
import type { SortConfig } from "@/protoFleet/api/generated/common/v1/sort_pb";
import type { InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import type {
  MinerListFilter,
  MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import ModuleActionsMenu from "@/protoFleet/features/containers/ModuleActionsMenu";
import FleetListActionBar from "@/protoFleet/features/fleetManagement/components/ActionBar/FleetListActionBar";
import useActionBarLifecycle from "@/protoFleet/features/fleetManagement/components/ActionBar/useActionBarLifecycle";
import MinerActionsMenu from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu";
import { ActionBarSelectionControls } from "@/shared/components/ActionBar";
import { type SelectionMode } from "@/shared/components/List";

interface MinerListActionBarProps {
  selectedKeys: MiningSelectionKey[];
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
  selectedKeys,
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
  const { isActionBusy, onActionStart, onActionComplete } = useActionBarLifecycle(selectedKeys.length);
  const selectionRef = useRef({ keys: selectedKeys, modules });
  if (!isActionBusy) {
    selectionRef.current = { keys: selectedKeys, modules };
  }
  const { keys: activeSelectedKeys, modules: activeModules } = selectionRef.current;
  const minerIdentifiers = selectedMinerIdentifiers(activeSelectedKeys);
  const itemNoun =
    activeModules.length > 0
      ? minerIdentifiers.length > 0
        ? { singular: "mining asset", plural: "mining assets" }
        : { singular: "module", plural: "modules" }
      : { singular: "miner", plural: "miners" };
  const effectiveSelectionMode =
    selectionMode === "all" && (selectAllOnPage || activeModules.length > 0) ? "subset" : selectionMode;

  const selectionControls =
    onSelectAll || onSelectNone ? (
      <ActionBarSelectionControls
        onSelectAll={onSelectAll}
        onSelectNone={onSelectNone}
        visibleOnly={selectAllOnPage || activeModules.length > 0}
        selectAllTestId="select-all-miners-button"
        selectNoneTestId="select-none-miners-button"
      />
    ) : undefined;

  return (
    <FleetListActionBar
      isActionBusy={isActionBusy}
      selectedItems={activeSelectedKeys}
      itemNoun={itemNoun}
      selectionMode={effectiveSelectionMode}
      totalCount={totalCount}
      filtersActive={filtersActive}
      onClose={onClearSelection}
      selectionControls={selectionControls}
    >
      <div className="flex items-center gap-2">
        {activeModules.length > 0 ? (
          <ModuleActionsMenu
            modules={activeModules}
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
    </FleetListActionBar>
  );
};

export default MinerListActionBar;
