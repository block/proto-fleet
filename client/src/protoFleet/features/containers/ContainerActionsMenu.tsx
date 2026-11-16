import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import ContainerActionDialog from "./ContainerActionDialog";
import {
  CONTAINER_ACTIONS,
  type ContainerAction,
  containerMiningCommand,
  requestContainerAction,
} from "./containerActions";
import ContainerSiteDialog from "./ContainerSiteDialog";
import ContainerUnpairDialog from "./ContainerUnpairDialog";
import { FRIGATE_UPDATE_PERMISSION } from "./frigateUpdate";
import FrigateUpdateDialog from "./FrigateUpdateDialog";
import { hasMiningGroupPermissions } from "./miningGroupPermissions";
import MiningGroupsDialog from "./MiningGroupsDialog";
import { isCurrentObservation } from "./moduleDetail";
import { useMiningGroupState } from "./useMiningGroupState";
import { controllerLogsPath, useOpenContainerView } from "./useOpenContainerView";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import type { UpdateJob } from "@/protoFleet/api/generated/frigateupdate/v1/frigateupdate_pb";
import { FirmwareUpdateRequestSchema } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { CT1_FIRMWARE_TARGET } from "@/protoFleet/api/useFirmwareApi";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import {
  ACTION_PERMISSIONS,
  hasAllRequired,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/actionPermissions";
import {
  deviceActions,
  settingsActions,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/constants";
import FirmwareUpdateModal from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/FirmwareUpdateModal";
import { ConfigurationSelector } from "@/protoFleet/features/poolConfigurations/ConfigurationSelector";
import { usePermissions } from "@/protoFleet/store";
import {
  ArrowRight,
  Download,
  MiningPools,
  Play,
  Power,
  Settings,
  Speedometer,
  Terminal,
  Unpair,
} from "@/shared/assets/icons";
import RowActionsMenu, { type RowAction } from "@/shared/components/RowActionsMenu";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const unavailableSettings = [
  { label: "Update worker name", action: settingsActions.updateWorkerNames, icon: <MiningPools /> },
];

export default function ContainerActionsMenu({
  container,
  onSiteChanged,
  onUnpaired,
  onFrigateUpdateStarted,
}: {
  container: Container;
  onSiteChanged?: () => void;
  onUnpaired?: (deviceIdentifier: string) => void;
  onFrigateUpdateStarted?: (job: UpdateJob | undefined) => void;
}) {
  const permissions = usePermissions();
  const { firmwareUpdate } = useMinerCommand();
  const hasActionPermissions = (action: keyof typeof ACTION_PERMISSIONS) =>
    hasAllRequired(ACTION_PERMISSIONS[action], permissions);
  const [firmwareOpen, setFirmwareOpen] = useState(false);
  const canUpdateFirmware = permissions.includes("miner:read") && hasActionPermissions(deviceActions.firmwareUpdate);
  const [groupsOpen, setGroupsOpen] = useState(false);
  const [groupsMode, setGroupsMode] = useState<"activate" | "deactivate">("activate");
  const [poolTarget, setPoolTarget] = useState<string>();
  const openContainerView = useOpenContainerView();
  const navigate = useNavigate();
  const [action, setAction] = useState<ContainerAction>();
  const [unpairOpen, setUnpairOpen] = useState(false);
  const [frigateUpdateOpen, setFrigateUpdateOpen] = useState(false);
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
  const groupsAction = groups?.canResumeDeactivation || groupsEnabled ? "deactivate" : "activate";
  let groupsLabel = groupsEnabled ? "Disable mining groups" : "Enable mining groups";
  if (groups?.canResumeDeactivation) groupsLabel = "Resume disabling mining groups";
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
          // Fleet checks the same permission at the container's site before reading.
          label: "Controller logs",
          icon: <Terminal />,
          hidden: !hasActionPermissions(deviceActions.downloadLogs),
          onClick: () => navigate(controllerLogsPath(container)),
        },
        {
          label: groupsLabel,
          icon: <MiningPools />,
          hidden: !hasMiningGroupPermissions(permissions, groupsAction) || !groups?.supported,
          onClick: () => {
            setGroupsMode(groupsAction);
            setGroupsOpen(true);
          },
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
        {
          label: "Update firmware",
          icon: <Settings />,
          hidden: !canUpdateFirmware,
          onClick: () => setFirmwareOpen(true),
        },
        ...unavailableSettings
          .filter((entry) => hasActionPermissions(entry.action))
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

  // Updating the controllers' software is its own permission, independent of miner access.
  if (permissions.includes(FRIGATE_UPDATE_PERMISSION))
    actions.push({
      label: "Update Frigate",
      icon: <Download />,
      showGroupDivider: true,
      onClick: () => setFrigateUpdateOpen(true),
    });

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
      {firmwareOpen && canUpdateFirmware ? (
        <FirmwareUpdateModal
          open
          target={CT1_FIRMWARE_TARGET}
          onDismiss={() => setFirmwareOpen(false)}
          onConfirm={(firmwareFileId) => {
            setFirmwareOpen(false);
            void firmwareUpdate({
              firmwareUpdateRequest: create(FirmwareUpdateRequestSchema, {
                firmwareFileId,
                deviceSelector: {
                  selectionType: { case: "includeDevices", value: { deviceIdentifiers: [deviceIdentifier] } },
                },
              }),
              onSuccess: () =>
                pushToast({
                  message: "Hashboard firmware update queued. Board progress is available in module details.",
                  status: STATUSES.success,
                }),
              onError: (message) => pushToast({ message, status: STATUSES.error }),
            });
          }}
        />
      ) : null}
      {groupsOpen ? (
        <MiningGroupsDialog
          deviceIdentifier={deviceIdentifier}
          mode={groupsMode}
          onClose={() => setGroupsOpen(false)}
        />
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
      {frigateUpdateOpen && permissions.includes(FRIGATE_UPDATE_PERMISSION) ? (
        <FrigateUpdateDialog
          key={deviceIdentifier}
          container={container}
          onClose={() => setFrigateUpdateOpen(false)}
          onStarted={onFrigateUpdateStarted}
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
