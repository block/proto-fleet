import { useCallback, useRef, useState } from "react";

import type { MinerEligibility, MinerSelectionListHandle } from "@/protoFleet/components/MinerSelectionList";
import MinerSelectionList from "@/protoFleet/components/MinerSelectionList";
import type { SiteFilterFields } from "@/protoFleet/components/PageHeader/SitePicker";
import { FLEET_VISIBLE_PAIRING_STATUSES } from "@/protoFleet/features/fleetManagement/utils/fleetVisiblePairingFilter";

import Modal from "@/shared/components/Modal";

interface SearchMinersModalProps {
  show: boolean;
  /** Target rack placement. Drives the "Show assignable only" toggle and the
   *  id-based eligibility filter (miners in another rack/building/site drop out
   *  or render disabled). */
  eligibility: MinerEligibility;
  /** Target rack label, shown in the assignment-conflict dialog. */
  targetRackLabel: string;
  /** Header SitePicker scope. Limits the list (and its Building/Rack facets) to
   *  the active site so the modal never shows the full org when a site is
   *  scoped; "all sites" passes the empty filter and shows everything. */
  scope?: SiteFilterFields;
  onDismiss: () => void;
  /** `isReassignment` is true when the picked miner is currently assigned to a
   *  different rack/building/site, so the caller can confirm the reparent. */
  onConfirm: (selectedMinerId: string, isReassignment: boolean) => void;
}

export default function SearchMinersModal({
  show,
  eligibility,
  targetRackLabel,
  scope,
  onDismiss,
  onConfirm,
}: SearchMinersModalProps) {
  const selectionRef = useRef<MinerSelectionListHandle>(null);
  const [hasSelection, setHasSelection] = useState(false);

  const handleConfirm = useCallback(() => {
    const selection = selectionRef.current?.getSelection();
    // blockedByFilter: a conflicting placement facet is showing no results, so
    // the (hidden) selection must not be acted on.
    if (!selection || selection.blockedByFilter || selection.selectedItems.length === 0) return;
    const minerId = selection.selectedItems[0];
    onConfirm(minerId, selection.reassignedItems.includes(minerId));
  }, [onConfirm]);

  if (!show) return null;

  return (
    <Modal
      open={show}
      title="Search miners"
      size="large"
      onDismiss={onDismiss}
      divider={false}
      buttons={[
        {
          text: "Assign",
          variant: "primary",
          disabled: !hasSelection,
          onClick: handleConfirm,
          dismissModalOnClick: false,
        },
      ]}
    >
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
        eligibility={eligibility}
        targetRackLabel={targetRackLabel}
        pairingStatuses={FLEET_VISIBLE_PAIRING_STATUSES}
        singleSelect
        onSelectionChange={({ selectedItems }) => setHasSelection(selectedItems.length > 0)}
      />
    </Modal>
  );
}
