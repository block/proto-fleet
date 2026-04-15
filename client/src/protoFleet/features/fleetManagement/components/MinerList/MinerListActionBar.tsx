import { useEffect, useRef } from "react";
import type { SortConfig } from "@/protoFleet/api/generated/common/v1/sort_pb";
import type {
  MinerListFilter,
  MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import ActionBar from "@/protoFleet/features/fleetManagement/components/ActionBar";
import MinerActionsMenu from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu";
import { useSetActionBarVisible } from "@/protoFleet/store";
import Button, { sizes, variants } from "@/shared/components/Button";
import { type SelectionMode } from "@/shared/components/List";

interface MinerListActionBarProps {
  selectedMiners: string[];
  onClearSelection?: () => void;
  onSelectAll?: () => void;
  onSelectNone?: () => void;
  selectionMode: SelectionMode;
  totalCount?: number;
  currentFilter?: MinerListFilter;
  currentSort?: SortConfig;
  miners?: Record<string, MinerStateSnapshot>;
  minerIds?: string[];
  onRefetchMiners?: () => void;
  onWorkerNameUpdated?: (deviceIdentifier: string, workerName: string) => void;
}

const MinerListActionBar = ({
  selectedMiners,
  onClearSelection,
  onSelectAll,
  onSelectNone,
  selectionMode,
  totalCount,
  currentFilter,
  currentSort,
  miners,
  minerIds,
  onRefetchMiners,
  onWorkerNameUpdated,
}: MinerListActionBarProps) => {
  const setActionBarVisible = useSetActionBarVisible();
  const selectedMinersCountRef = useRef(selectedMiners.length);

  useEffect(() => {
    selectedMinersCountRef.current = selectedMiners.length;
    setActionBarVisible(selectedMiners.length > 0);
  }, [selectedMiners.length, setActionBarVisible]);

  useEffect(() => {
    return () => setActionBarVisible(false);
  }, [setActionBarVisible]);

  const selectionControls =
    onSelectAll || onSelectNone ? (
      <>
        {onSelectAll ? (
          <Button
            className="py-1"
            size={sizes.textOnly}
            variant={variants.textOnly}
            textColor="text-core-accent-fill"
            textOnlyUnderlineOnHover={false}
            testId="select-all-miners-button"
            onClick={onSelectAll}
          >
            Select all
          </Button>
        ) : null}
        {onSelectNone ? (
          <Button
            className="py-1"
            size={sizes.textOnly}
            variant={variants.textOnly}
            textColor="text-core-accent-fill"
            textOnlyUnderlineOnHover={false}
            testId="select-none-miners-button"
            onClick={onSelectNone}
          >
            Select none
          </Button>
        ) : null}
      </>
    ) : undefined;

  return (
    <ActionBar
      className="fixed right-0 bottom-4 left-16 z-20 phone:left-0 tablet:left-0 desktop:left-50"
      selectedItems={selectedMiners}
      selectionMode={selectionMode}
      totalCount={totalCount}
      onClose={onClearSelection}
      selectionControls={selectionControls}
      renderActions={(setHidden) => (
        <MinerActionsMenu
          selectedMiners={selectedMiners}
          selectionMode={selectionMode}
          totalCount={totalCount}
          currentFilter={currentFilter}
          currentSort={currentSort}
          miners={miners}
          minerIds={minerIds}
          onRefetchMiners={onRefetchMiners}
          onWorkerNameUpdated={onWorkerNameUpdated}
          onActionStart={() => {
            setHidden(true);
            setActionBarVisible(false);
          }}
          onActionComplete={() => {
            setHidden(false);
            setActionBarVisible(selectedMinersCountRef.current > 0);
          }}
        />
      )}
    />
  );
};

export default MinerListActionBar;
