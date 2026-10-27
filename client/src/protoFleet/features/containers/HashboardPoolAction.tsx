import { useEffect, useState } from "react";
import EditHashboardPoolDialog from "./EditHashboardPoolDialog";
import { hasMiningGroupPermissions } from "./miningGroupPermissions";
import { isCurrentObservation } from "./moduleDetail";
import { canEditHashboardPools, useMiningGroupState } from "./useMiningGroupState";
import type { HashboardSlot } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { usePermissions } from "@/protoFleet/store";
import { MiningPools } from "@/shared/assets/icons";
import ProgressCircular from "@/shared/components/ProgressCircular";
import RowActionsMenu from "@/shared/components/RowActionsMenu";

export default function HashboardPoolAction({
  deviceIdentifier,
  slot,
}: {
  deviceIdentifier: string;
  slot: HashboardSlot;
}) {
  const permissions = usePermissions();
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 4000);
    return () => clearInterval(timer);
  }, []);
  const [selectedBoardID, setSelectedBoardID] = useState<bigint>();
  const [menuOpen, setMenuOpen] = useState(false);
  const canEdit = hasMiningGroupPermissions(permissions, "pools") && permissions.includes("pool:read");
  const { state: groups, isFetching } = useMiningGroupState(deviceIdentifier, menuOpen && canEdit);
  if (!canEdit) return null;
  const board = slot.occupancy === "occupied" ? slot.board : undefined;
  const available = !!board?.hashboardId && !slot.unavailableReason && isCurrentObservation(slot.observedAt, now);
  return (
    <div onClick={(e) => e.stopPropagation()}>
      <RowActionsMenu
        ariaLabel={`Actions for hashboard ${slot.position}`}
        onOpenChange={setMenuOpen}
        actions={[
          {
            label: "Edit pool",
            icon:
              available && isFetching && !canEditHashboardPools(groups) ? (
                <ProgressCircular indeterminate size={18} dataTestId="pool-readiness-spinner" />
              ) : (
                <MiningPools />
              ),
            hidden: groups?.supported === false,
            disabled: !available || !canEditHashboardPools(groups),
            onClick: () => setSelectedBoardID(board?.hashboardId),
          },
        ]}
      />
      {selectedBoardID !== undefined ? (
        <EditHashboardPoolDialog
          key={`${deviceIdentifier}:${selectedBoardID}`}
          deviceIdentifier={deviceIdentifier}
          hashboardIds={[selectedBoardID]}
          onClose={() => setSelectedBoardID(undefined)}
        />
      ) : null}
    </div>
  );
}
