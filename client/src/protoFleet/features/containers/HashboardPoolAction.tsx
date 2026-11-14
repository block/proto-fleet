import { useEffect, useState } from "react";
import BoardIdentifyDialog from "./BoardIdentifyDialog";
import EditHashboardPoolDialog from "./EditHashboardPoolDialog";
import { hasMiningGroupPermissions } from "./miningGroupPermissions";
import { isCurrentObservation } from "./moduleDetail";
import { canEditHashboardPools, useMiningGroupState } from "./useMiningGroupState";
import type { HashboardSlot } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { WorkerNameDialog } from "@/protoFleet/features/poolConfigurations/WorkerNameDialog";
import { usePermissions } from "@/protoFleet/store";
import { LEDIndicator, MiningPools } from "@/shared/assets/icons";
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
  const [identify, setIdentify] = useState<{ hashboardId: bigint; blink: boolean }>();
  const [workerBoardID, setWorkerBoardID] = useState<bigint>();
  const [menuOpen, setMenuOpen] = useState(false);
  const canRename =
    hasMiningGroupPermissions(permissions, "pools") &&
    permissions.includes("miner:update_worker_names") &&
    permissions.includes("miner:read") &&
    permissions.includes("pool:read");
  const canEdit = hasMiningGroupPermissions(permissions, "pools") && permissions.includes("pool:read");
  const canIdentify = permissions.includes("miner:blink_led");
  const { state: groups, isFetching } = useMiningGroupState(deviceIdentifier, menuOpen && (canEdit || canRename));
  if (!canEdit && !canRename && !canIdentify) return null;
  const board = slot.occupancy === "occupied" ? slot.board : undefined;
  const available = !!board?.hashboardId && !slot.unavailableReason && isCurrentObservation(slot.observedAt, now);
  return (
    <div onClick={(e) => e.stopPropagation()}>
      <RowActionsMenu
        ariaLabel={`Actions for hashboard ${slot.position}`}
        onOpenChange={setMenuOpen}
        actions={[
          {
            label: "Update worker name",
            icon: <MiningPools />,
            hidden: !canRename || groups?.supported === false,
            disabled: !available || !canEditHashboardPools(groups),
            onClick: () => setWorkerBoardID(board?.hashboardId),
          },
          {
            label: "Edit pool",
            icon:
              available && isFetching && !canEditHashboardPools(groups) ? (
                <ProgressCircular indeterminate size={18} dataTestId="pool-readiness-spinner" />
              ) : (
                <MiningPools />
              ),
            hidden: !canEdit || groups?.supported === false,
            disabled: !available || !canEditHashboardPools(groups),
            onClick: () => setSelectedBoardID(board?.hashboardId),
          },
          ...[true, false].map((blink) => ({
            label: blink ? "Blink LEDs" : "Stop blinking LEDs",
            icon: <LEDIndicator />,
            hidden: !canIdentify,
            disabled: !available,
            onClick: () => {
              if (board?.hashboardId) setIdentify({ hashboardId: board.hashboardId, blink });
            },
          })),
        ]}
      />
      {workerBoardID !== undefined ? (
        <WorkerNameDialog
          key={`${deviceIdentifier}:${workerBoardID}`}
          hashboardIds={[workerBoardID]}
          hashboards={board ? [board] : []}
          onDismiss={() => setWorkerBoardID(undefined)}
        />
      ) : null}
      {selectedBoardID !== undefined ? (
        <EditHashboardPoolDialog
          key={`${deviceIdentifier}:${selectedBoardID}`}
          deviceIdentifier={deviceIdentifier}
          hashboardIds={[selectedBoardID]}
          onClose={() => setSelectedBoardID(undefined)}
        />
      ) : null}
      {identify ? (
        <BoardIdentifyDialog
          key={`${identify.hashboardId}:${identify.blink}`}
          hashboardIds={[identify.hashboardId]}
          blink={identify.blink}
          onClose={() => setIdentify(undefined)}
        />
      ) : null}
    </div>
  );
}
