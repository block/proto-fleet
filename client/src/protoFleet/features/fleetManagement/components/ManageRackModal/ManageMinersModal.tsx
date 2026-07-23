import { useCallback, useRef, useState } from "react";

import { type MinerListFilter } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import MinerSelectionList, {
  type MinerEligibility,
  type MinerSelectionListHandle,
} from "@/protoFleet/components/MinerSelectionList";
import type { SiteFilterFields } from "@/protoFleet/components/PageHeader/SitePicker";
import { FLEET_VISIBLE_PAIRING_STATUSES } from "@/protoFleet/features/fleetManagement/utils/fleetVisiblePairingFilter";

import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal";

interface ManageMinersModalProps {
  show: boolean;
  currentRackMiners: string[];
  /** Target rack placement. Drives the "Show assignable only" toggle and the
   *  id-based eligibility filter. */
  eligibility: MinerEligibility;
  /** Target rack label, shown in the assignment-conflict dialog. */
  targetRackLabel: string;
  maxSlots: number;
  /** Header SitePicker scope. Limits the list (and its Building/Rack facets) to
   *  the active site so the modal never shows the full org when a site is
   *  scoped; "all sites" passes the empty filter and shows everything. */
  scope?: SiteFilterFields;
  onDismiss: () => void;
  /** `reassignedItems` is the subset of the explicit selection that is currently
   *  assigned elsewhere, so the caller can confirm the reparent (empty when
   *  `allSelected`, since that path is pre-filtered to assignable miners).
   *  Resolves to an error string to surface inside this (still-open) modal —
   *  e.g. select-all overflow, which the parent only knows after resolving the
   *  full id set — or undefined on success. */
  onConfirm: (
    selectedIds: string[],
    allSelected: boolean,
    filter: MinerListFilter | undefined,
    reassignedItems: string[],
  ) => Promise<string | undefined>;
}

export default function ManageMinersModal({
  show,
  currentRackMiners,
  eligibility,
  targetRackLabel,
  maxSlots,
  scope,
  onDismiss,
  onConfirm,
}: ManageMinersModalProps) {
  const selectionRef = useRef<MinerSelectionListHandle>(null);
  const [overflowError, setOverflowError] = useState("");

  const handleContinue = useCallback(async () => {
    const selection = selectionRef.current?.getSelection();
    if (!selection) return;

    const { selectedItems, allSelected, filter, reassignedItems, blockedByFilter } = selection;
    setOverflowError("");

    // A conflicting placement facet shows "no results"; committing here would
    // save a selection the operator can't see (or wipe membership). Prompt them
    // to clear the filter first instead.
    if (blockedByFilter) {
      setOverflowError("Clear the Building or Rack filter to continue — it doesn't match this rack.");
      return;
    }

    // Only validate overflow for explicit selections. When allSelected is true,
    // the parent resolves the full selectable list via server pagination, so it
    // returns the overflow (or load) error for us to surface here — this modal
    // is still mounted above the parent's callout.
    if (!allSelected && selectedItems.length > maxSlots) {
      setOverflowError(
        `Cannot add ${selectedItems.length} miners with only ${maxSlots} available slots. Deselect some miners or update your rack settings.`,
      );
      return;
    }

    const error = await onConfirm(selectedItems, allSelected, allSelected ? filter : undefined, reassignedItems);
    if (error) setOverflowError(error);
  }, [maxSlots, onConfirm]);

  if (!show) return null;

  return (
    <Modal
      open={show}
      title="Select miners"
      size="large"
      className="flex !h-[calc(100dvh-(--spacing(32)))] max-h-[calc(100dvh-(--spacing(32)))] flex-col !overflow-hidden"
      bodyClassName="flex flex-1 min-h-0 flex-col overflow-hidden"
      onDismiss={onDismiss}
      divider={false}
      buttons={[
        {
          text: "Continue",
          variant: "primary",
          onClick: handleContinue,
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="flex h-full min-h-0 flex-col">
        {overflowError ? (
          <Callout className="mb-4 shrink-0" intent="danger" prefixIcon={<Alert />} title={overflowError} />
        ) : null}

        <MinerSelectionList
          ref={selectionRef}
          filterConfig={{
            showTypeFilter: true,
            showSubnetFilter: true,
            // Site facet is redundant when the header SitePicker scope governs
            // the site, so hide it whenever a `scope` is supplied. If a caller
            // omits scope, keep the facet so the picker can still narrow by
            // site (rather than stranding the operator on the full org list).
            showSiteFilter: !scope,
            showBuildingFilter: true,
            showRackFilter: true,
            showGroupFilter: true,
          }}
          scope={scope}
          initialSelectedItems={currentRackMiners}
          eligibility={eligibility}
          targetRackLabel={targetRackLabel}
          pairingStatuses={FLEET_VISIBLE_PAIRING_STATUSES}
        />
      </div>
    </Modal>
  );
}
