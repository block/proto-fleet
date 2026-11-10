import { useEffect, useState } from "react";
import ContainerActionDialog from "./ContainerActionDialog";
import {
  CONTAINER_ACTIONS,
  type ContainerAction,
  containerMiningCommand,
  requestContainerAction,
} from "./containerActions";
import ContainerSiteDialog from "./ContainerSiteDialog";
import ContainerUnpairDialog from "./ContainerUnpairDialog";
import { hasMiningGroupPermissions } from "./miningGroupPermissions";
import MiningGroupsDialog from "./MiningGroupsDialog";
import { isCurrentObservation } from "./moduleDetail";
import { useMiningGroupState } from "./useMiningGroupState";
import { useOpenContainerView } from "./useOpenContainerView";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { ACTION_PERMISSIONS } from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/actionPermissions";
import {
  deviceActions,
  settingsActions,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/constants";
import { ConfigurationSelector } from "@/protoFleet/features/poolConfigurations/ConfigurationSelector";
import { usePermissions } from "@/protoFleet/store";
import { ArrowRight, MiningPools, Play, Power, Settings, Speedometer, Unpair } from "@/shared/assets/icons";
import RowActionsMenu, { type RowAction } from "@/shared/components/RowActionsMenu";

// Firmware and standalone worker-name updates are not available for CT1.
const unavailableSettings = [
  { label: "Update firmware", action: deviceActions.firmwareUpdate, icon: <Settings /> },
  { label: "Update worker name", action: settingsActions.updateWorkerNames, icon: <MiningPools /> },
];

export default function ContainerActionsMenu({
  container,
  onSiteChanged,
  onUnpaired,
}: {
  container: Container;
  onSiteChanged?: () => void;
  onUnpaired?: (deviceIdentifier: string) => void;
}) {
  const permissions = usePermissions();
  const [groupsOpen, setGroupsOpen] = useState(false);
  const [poolTarget, setPoolTarget] = useState<string>();
  const openContainerView = useOpenContainerView();
  const [action, setAction] = useState<ContainerAction>();
  const [unpairOpen, setUnpairOpen] = useState(false);
  const [open, setOpen] = useState(false);
  const [assigningSite, setAssigningSite] = useState(false);
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
  const { state: groups } = useMiningGroupState(deviceIdentifier, open && permissions.includes("miner:read"));
  const groupsEnabled = groups?.enabled;
  const canEditPools = permissions.includes("pool:read") && permissions.includes("miner:update_pools");
  const poolsReady =
    !!groups?.supported &&
    !groups.suspended &&
    isCurrentObservation(groups.observedAt) &&
    !["pending", "applying", "blocked"].includes(groups.status);

  useEffect(() => {
    if (!open || !hasControls) return;
    const abort = new AbortController();
    void requestContainerAction(deviceIdentifier, "mining/status", "GET", undefined, abort.signal)
      .then((data) => {
        if (!abort.signal.aborted)
          setStatus({
            deviceIdentifier,
            available: true,
            command: containerMiningCommand(data),
          });
      })
      .catch(() => {
        if (!abort.signal.aborted) setStatus({ deviceIdentifier, available: false });
      });
    return () => abort.abort();
  }, [open, deviceIdentifier, hasControls]);

  const commands: RowAction[] = permittedActions
    .filter((entry) => entry.kind === "command" && entry.path === currentStatus?.command)
    .map((entry) => {
      const verb = entry.path === "mining/start" ? "Wake" : "Sleep";
      const label = groupsEnabled === undefined ? verb : groupsEnabled ? `${verb} default mining group` : entry.label;
      return {
        label,
        icon: entry.path === "mining/start" ? <Play /> : <Power />,
        showGroupDivider: true,
        onClick: () => setAction({ ...entry, label }),
      };
    });
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
        {
          label: "Enable mining groups",
          icon: <MiningPools />,
          hidden: !hasMiningGroupPermissions(permissions, "activate") || !groups?.supported || groups.enabled,
          onClick: () => setGroupsOpen(true),
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
        {
          label: groupsEnabled ? "Container pool assignment" : "Edit pool",
          icon: <MiningPools />,
          hidden: !canEditPools,
          disabled: !poolsReady,
          onClick: () => setPoolTarget(deviceIdentifier),
        },
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
        {
          label: "Unpair",
          icon: <Unpair />,
          hidden: !permissions.includes("miner:delete"),
          onClick: () => setUnpairOpen(true),
        },
      ]
    : [];

  if (container.canAssignSite)
    actions.push({
      label: "Change site",
      icon: <Settings />,
      showGroupDivider: true,
      onClick: () => setAssigningSite(true),
    });

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
          if (nextOpen) {
            setStatus(undefined);
          }
          setOpen(nextOpen);
        }}
      />
      {poolTarget === deviceIdentifier && canEditPools && permissions.includes("miner:read") ? (
        <ConfigurationSelector
          key={poolTarget}
          deviceIdentifiers={[poolTarget]}
          removalOnly={groupsEnabled}
          selectionDescription="Applies to the whole container."
          onDismiss={() => setPoolTarget(undefined)}
        />
      ) : null}
      {groupsOpen ? (
        <MiningGroupsDialog deviceIdentifier={deviceIdentifier} onClose={() => setGroupsOpen(false)} />
      ) : null}
      {assigningSite ? (
        <ContainerSiteDialog container={container} onClose={() => setAssigningSite(false)} onSaved={onSiteChanged} />
      ) : null}
      {action ? (
        <ContainerActionDialog
          key={`${container.deviceIdentifier}:${action.path}`}
          container={container}
          action={action}
          miningScope={groupsEnabled === undefined ? "unknown" : groupsEnabled ? "default-group" : "container"}
          onClose={() => setAction(undefined)}
        />
      ) : null}
      {unpairOpen ? (
        <ContainerUnpairDialog
          key={container.deviceIdentifier}
          container={container}
          onClose={() => setUnpairOpen(false)}
          onUnpaired={onUnpaired}
        />
      ) : null}
    </div>
  );
}
