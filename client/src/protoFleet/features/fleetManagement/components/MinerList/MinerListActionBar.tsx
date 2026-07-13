import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";
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
  onClearSelection,
  onSelectAll,
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

  // Portal to the body so the bar escapes the app shell's fixed `z-20` scroll
  // container (a stacking context). Otherwise the bulk-actions menu — which opens
  // upward into the header band — is trapped below the `z-40` shell header no
  // matter how high we set the bar's own z-index (#727). z-[45] sits above the
  // header but below the phone/tablet nav drawer (`z-50`) so the drawer still
  // covers the bar when open.
  return createPortal(
    <ActionBar
      className="fixed right-0 bottom-4 left-0 z-[45] laptop:left-16 desktop:left-50"
      selectedItems={selectedMiners}
      selectionMode={selectionMode}
      totalCount={totalCount}
      filtersActive={filtersActive}
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
          selectionIncludesUnauthenticatedMiner={selectionIncludesUnauthenticatedMiner}
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
    />,
    document.body,
  );
};

export default MinerListActionBar;
