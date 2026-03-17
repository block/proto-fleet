import type { SortConfig } from "@/protoFleet/api/generated/common/v1/sort_pb";
import type { MinerListFilter } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import ActionBar from "@/protoFleet/features/fleetManagement/components/ActionBar";
import MinerActionsMenu from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu";
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
}: MinerListActionBarProps) => {
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
      className="fixed bottom-4 z-20"
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
          onActionStart={() => setHidden(true)}
          onActionComplete={() => setHidden(false)}
        />
      )}
    />
  );
};

export default MinerListActionBar;
