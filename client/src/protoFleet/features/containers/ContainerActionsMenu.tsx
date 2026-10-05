import { useEffect, useState } from "react";
import ContainerActionDialog from "./ContainerActionDialog";
import {
  CONTAINER_ACTIONS,
  type ContainerAction,
  containerMiningCommand,
  requestContainerAction,
} from "./containerActions";
import { useOpenContainerView } from "./useOpenContainerView";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { ACTION_PERMISSIONS } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/actionPermissions";
import {
  deviceActions,
  settingsActions,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/constants";
import { usePermissions } from "@/protoFleet/store";
import { ArrowRight, MiningPools, Play, Power, Settings, Speedometer } from "@/shared/assets/icons";
import RowActionsMenu, { type RowAction } from "@/shared/components/RowActionsMenu";

// CT1 does not implement Fleet's pool configuration or firmware capabilities.
// Worker-name updates also require pool configuration to reapply the credentials.
const unavailableSettings = [
  { label: "Update firmware", action: deviceActions.firmwareUpdate, icon: <Settings /> },
  { label: "Edit pool", action: settingsActions.miningPool, icon: <MiningPools /> },
  { label: "Update worker name", action: settingsActions.updateWorkerNames, icon: <MiningPools /> },
];

export default function ContainerActionsMenu({ container }: { container: Container }) {
  const permissions = usePermissions();
  const openContainerView = useOpenContainerView();
  const [action, setAction] = useState<ContainerAction>();
  const [open, setOpen] = useState(false);
  const [status, setStatus] = useState<{
    deviceIdentifier: string;
    available: boolean;
    command?: "mining/start" | "mining/stop";
  }>();
  const permittedActions = CONTAINER_ACTIONS.filter(
    (entry) => permissions.includes(entry.permission) && permissions.includes("miner:read"),
  );
  const hasMiningActions = permittedActions.some((entry) => entry.kind === "command");
  const hasControls = permittedActions.length > 0;
  const { deviceIdentifier } = container;
  const currentStatus = status?.deviceIdentifier === deviceIdentifier ? status : undefined;

  useEffect(() => {
    if (!open || !hasControls) return;
    const abort = new AbortController();
    void requestContainerAction(deviceIdentifier, "mining/status", "GET", undefined, abort.signal)
      .then((data) => {
        if (!abort.signal.aborted)
          setStatus({ deviceIdentifier, available: true, command: containerMiningCommand(data) });
      })
      .catch(() => {
        if (!abort.signal.aborted) setStatus({ deviceIdentifier, available: false });
      });
    return () => abort.abort();
  }, [open, deviceIdentifier, hasControls]);

  const commands: RowAction[] = permittedActions
    .filter((entry) => entry.kind === "command" && entry.path === currentStatus?.command)
    .map((entry) => ({
      label: entry.label,
      icon: entry.path === "mining/start" ? <Play /> : <Power />,
      showGroupDivider: true,
      onClick: () => setAction(entry),
    }));
  if (hasMiningActions && commands.length === 0) {
    commands.push({
      label:
        currentStatus && !currentStatus.command
          ? "Container status unavailable"
          : currentStatus?.command
            ? "No applicable commands"
            : "Checking container status…",
      disabled: true,
      icon: <Power />,
      showGroupDivider: true,
      onClick: () => undefined,
    });
  }

  const actions: RowAction[] = permissions.includes("miner:read")
    ? [
        {
          label: "View container",
          icon: <ArrowRight className="text-text-primary" />,
          showGroupDivider: true,
          onClick: () => openContainerView(container),
        },
        ...commands,
        ...permittedActions
          .filter((entry) => entry.kind === "power")
          .map((entry) => ({
            label: entry.label,
            icon: <Speedometer />,
            disabled: !currentStatus?.available,
            onClick: () => setAction(entry),
          })),
        ...unavailableSettings
          .filter((entry) => {
            const required = ACTION_PERMISSIONS[entry.action];
            return (typeof required === "string" ? [required] : required).every((key) => permissions.includes(key));
          })
          .map((entry) => ({
            label: entry.label,
            icon: entry.icon,
            disabled: true,
            onClick: () => undefined,
          })),
      ]
    : [];

  return (
    <div
      title={currentStatus?.available === false ? "Container controls are unavailable through Fleet." : undefined}
      onClick={(event) => event.stopPropagation()}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") event.stopPropagation();
      }}
    >
      <RowActionsMenu
        ariaLabel={`Actions for ${container.name}`}
        testIdPrefix={`container-${container.deviceIdentifier}-actions`}
        triggerClassName="!size-8 !rounded-full !p-0 text-text-primary-70"
        actions={actions}
        onOpenChange={(nextOpen) => {
          if (nextOpen) setStatus(undefined);
          setOpen(nextOpen);
        }}
      />
      {action ? (
        <ContainerActionDialog
          key={`${container.deviceIdentifier}:${action.path}`}
          container={container}
          action={action}
          onClose={() => setAction(undefined)}
        />
      ) : null}
    </div>
  );
}
