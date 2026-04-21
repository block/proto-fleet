import { useCallback, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { getLoadingMessage, minersMessage, settingsActions, SupportedAction } from "./constants";
import { type MinerGroup } from "./ManageSecurity";
import {
  type MinerListFilter,
  type MinerModelGroup,
  type MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  DeviceFilterSchema,
  DeviceSelector,
  DeviceSelectorSchema,
  UpdateMinerPasswordResponse,
} from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { minerTypes } from "@/protoFleet/features/fleetManagement/components/MinerList/constants";
import { createDeviceSelector } from "@/protoFleet/features/fleetManagement/utils/deviceSelector";
import { type SelectionMode } from "@/shared/components/List";
import { pushToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";

type PendingActionCallback = (filteredSelector?: DeviceSelector, filteredDeviceIds?: string[]) => void;

function groupMinersByModel(deviceIds: string[], miners: Record<string, MinerStateSnapshot>): MinerGroup[] {
  const groupMap = new Map<string, MinerGroup>();

  deviceIds.forEach((id) => {
    const miner = miners[id];
    if (!miner) return;

    const manufacturer = miner.manufacturer || "";
    const model = miner.model || "Unknown Model";
    const key = `${manufacturer}-${model}`;

    if (!groupMap.has(key)) {
      groupMap.set(key, {
        name: miner.name || model,
        model,
        manufacturer,
        count: 0,
        deviceIdentifiers: [],
        status: "pending",
      });
    }

    const group = groupMap.get(key)!;
    group.count++;
    group.deviceIdentifiers.push(id);
  });

  return Array.from(groupMap.values());
}

function updateGroupsAfterBatch(
  prev: MinerGroup[],
  groupSnapshot: MinerGroup,
  successIds: string[],
  failureIds: string[],
): MinerGroup[] {
  const rest = prev.filter(
    (g) =>
      !(g.manufacturer === groupSnapshot.manufacturer && g.model === groupSnapshot.model && g.status === "loading"),
  );

  if (successIds.length > 0 && failureIds.length > 0) {
    return [
      ...rest,
      { ...groupSnapshot, deviceIdentifiers: successIds, count: successIds.length, status: "updated" as const },
      { ...groupSnapshot, deviceIdentifiers: failureIds, count: failureIds.length, status: "pending" as const },
    ];
  }
  if (successIds.length > 0) {
    return [...rest, { ...groupSnapshot, status: "updated" as const }];
  }
  return [...rest, { ...groupSnapshot, status: "failed" as const }];
}

export interface SecurityActionsProps {
  showAuthenticateFleetModal: boolean;
  authenticationPurpose: "security" | "pool" | null;
  showUpdatePasswordModal: boolean;
  hasThirdPartyMiners: boolean;
  handleFleetAuthenticated: (username: string, password: string) => void;
  handlePasswordConfirm: (currentPassword: string, newPassword: string) => void;
  handlePasswordDismiss: () => void;
  handleAuthDismiss: () => void;
  showManageSecurityModal: boolean;
  minerGroups: MinerGroup[];
  handleUpdateGroup: (group: MinerGroup) => void;
  handleSecurityModalClose: () => void;
}

interface UseManageSecurityFlowParams {
  deviceIdentifiers: string[];
  selectionMode: SelectionMode;
  getMinerModelGroups: (filter: MinerListFilter | null) => Promise<MinerModelGroup[]>;
  withCapabilityCheck: (action: SupportedAction, onProceed: PendingActionCallback) => Promise<void>;
  updateMinerPassword: (params: {
    deviceSelector: DeviceSelector;
    newPassword: string;
    currentPassword: string;
    userUsername: string;
    userPassword: string;
    onSuccess: (value: UpdateMinerPasswordResponse) => void;
    onError?: (error: string) => void;
  }) => void;
  startBatchOperation: (batch: {
    batchIdentifier: string;
    action: SupportedAction;
    deviceIdentifiers: string[];
  }) => void;
  handleSuccess: (
    action: SupportedAction,
    originalToastId: number,
    batchIdentifier: string,
    onBatchComplete?: (successDeviceIds: string[], failureDeviceIds: string[]) => void,
  ) => void;
  handleError: (toastId: number, error: string) => void;
  onActionComplete?: () => void;
  setCurrentAction: (action: SupportedAction | null) => void;
  fleetCredentials: { username: string; password: string } | undefined;
  resetAuthState: () => void;
  miners?: Record<string, MinerStateSnapshot>;
  currentFilter?: MinerListFilter;
}

export const useManageSecurityFlow = ({
  deviceIdentifiers,
  selectionMode,
  getMinerModelGroups,
  withCapabilityCheck,
  updateMinerPassword,
  startBatchOperation,
  handleSuccess,
  handleError,
  onActionComplete,
  setCurrentAction,
  fleetCredentials,
  resetAuthState,
  miners = {} as Record<string, MinerStateSnapshot>,
  currentFilter,
}: UseManageSecurityFlowParams) => {
  const [showUpdatePasswordModal, setShowUpdatePasswordModal] = useState(false);
  const [securityFilteredDeviceIds, setSecurityFilteredDeviceIds] = useState<string[] | undefined>(undefined);
  const [hasThirdPartyMiners, setHasThirdPartyMiners] = useState(false);
  const [showManageSecurityModal, setShowManageSecurityModal] = useState(false);
  const [minerGroups, setMinerGroups] = useState<MinerGroup[]>([]);
  const [currentGroupForUpdate, setCurrentGroupForUpdate] = useState<MinerGroup | null>(null);

  // Resets security-specific state before starting the auth flow.
  const startManageSecurity = useCallback(() => {
    setSecurityFilteredDeviceIds(undefined);
    setCurrentAction(settingsActions.security);
  }, [setCurrentAction]);

  const openSecurityModalViaCapabilityCheck = useCallback(async () => {
    await withCapabilityCheck(settingsActions.security, (_filteredSelector, filteredDeviceIds) => {
      const deviceIdsToUse = filteredDeviceIds ?? deviceIdentifiers;
      setSecurityFilteredDeviceIds(filteredDeviceIds);
      setCurrentAction(settingsActions.security);
      setMinerGroups(groupMinersByModel(deviceIdsToUse, miners));
      setShowManageSecurityModal(true);
    });
  }, [withCapabilityCheck, deviceIdentifiers, setCurrentAction, miners]);

  // Called by useMinerActions once fleet auth completes with purpose="security".
  // Credentials are not needed here — they're read from the fleetCredentials param at confirm time.
  const handleSecurityAuthenticated = useCallback(
    async (_username: string, _password: string) => {
      if (selectionMode === "all") {
        // For "all" selection, query backend for accurate model groups across the full fleet
        try {
          const groups = await getMinerModelGroups(currentFilter ?? null);
          setMinerGroups(
            groups.map((g) => {
              const isProto = g.manufacturer.toLowerCase() === minerTypes.protoRig;
              return {
                name: isProto ? `${g.manufacturer} ${g.model}`.trim() : g.model,
                model: g.model,
                manufacturer: g.manufacturer,
                count: g.count,
                deviceIdentifiers: [],
                status: "pending" as const,
              };
            }),
          );
          setShowManageSecurityModal(true);
        } catch {
          await openSecurityModalViaCapabilityCheck();
        }
      } else {
        await openSecurityModalViaCapabilityCheck();
      }
    },
    [selectionMode, getMinerModelGroups, openSecurityModalViaCapabilityCheck, currentFilter],
  );

  const handleUpdateGroup = useCallback((group: MinerGroup) => {
    setCurrentGroupForUpdate(group);
    setHasThirdPartyMiners(group.manufacturer.toLowerCase() !== minerTypes.protoRig);
    setShowUpdatePasswordModal(true);
  }, []);

  const handleSecurityModalClose = useCallback(() => {
    setShowManageSecurityModal(false);
    setMinerGroups([]);
    setSecurityFilteredDeviceIds(undefined);
    setCurrentAction(null);
    resetAuthState();
    onActionComplete?.();
  }, [setCurrentAction, resetAuthState, onActionComplete]);

  const handlePasswordConfirm = useCallback(
    (currentPassword: string, newPassword: string) => {
      let selectorToUse: DeviceSelector;
      let deviceIdsToUse: string[];

      if (selectionMode === "all" && currentGroupForUpdate) {
        // For "all" selection, use a model-scoped all_devices selector so the command
        // targets every fleet miner of this model, not just the visible page.
        // Note: error_component_types filter has no equivalent in DeviceFilter and is not applied here.
        selectorToUse = create(DeviceSelectorSchema, {
          selectionType: {
            case: "allDevices",
            value: create(DeviceFilterSchema, {
              models: [currentGroupForUpdate.model],
              ...(currentGroupForUpdate.manufacturer ? { manufacturers: [currentGroupForUpdate.manufacturer] } : {}),
              deviceStatus: currentFilter?.deviceStatus ?? [],
              pairingStatus: currentFilter?.pairingStatuses ?? [],
            }),
          },
        });
        deviceIdsToUse = currentGroupForUpdate.deviceIdentifiers;
      } else {
        const rawDeviceIds = currentGroupForUpdate
          ? currentGroupForUpdate.deviceIdentifiers
          : (securityFilteredDeviceIds ?? deviceIdentifiers);
        selectorToUse = createDeviceSelector("subset", rawDeviceIds);
        deviceIdsToUse = rawDeviceIds;
      }

      if (!fleetCredentials) return;

      setShowUpdatePasswordModal(false);

      const id = pushToast({
        message: getLoadingMessage(settingsActions.security, minersMessage),
        status: TOAST_STATUSES.loading,
        longRunning: true,
        onClose: () => onActionComplete?.(),
      });

      updateMinerPassword({
        deviceSelector: selectorToUse,
        newPassword,
        currentPassword,
        userUsername: fleetCredentials.username,
        userPassword: fleetCredentials.password,
        onSuccess: (value: UpdateMinerPasswordResponse) => {
          startBatchOperation({
            batchIdentifier: value.batchIdentifier,
            action: settingsActions.security,
            deviceIdentifiers: deviceIdsToUse,
          });

          const groupSnapshot = currentGroupForUpdate;
          if (groupSnapshot) {
            setMinerGroups((prev) => prev.map((g) => (g === groupSnapshot ? { ...g, status: "loading" as const } : g)));
          }

          handleSuccess(
            settingsActions.security,
            id,
            value.batchIdentifier,
            groupSnapshot
              ? (successIds, failureIds) => {
                  setMinerGroups((prev) => updateGroupsAfterBatch(prev, groupSnapshot, successIds, failureIds));
                  setCurrentGroupForUpdate(null);
                }
              : () => onActionComplete?.(),
          );
        },
        onError: (error: string) => {
          handleError(id, error);

          if (currentGroupForUpdate) {
            setMinerGroups((prev) =>
              prev.map((g) => (g === currentGroupForUpdate ? { ...g, status: "failed" as const } : g)),
            );
            setCurrentGroupForUpdate(null);
          } else {
            onActionComplete?.();
          }
        },
      });

      setCurrentAction(null);
    },
    [
      selectionMode,
      currentGroupForUpdate,
      securityFilteredDeviceIds,
      deviceIdentifiers,
      fleetCredentials,
      updateMinerPassword,
      handleSuccess,
      handleError,
      onActionComplete,
      startBatchOperation,
      setCurrentAction,
      currentFilter,
    ],
  );

  const handlePasswordDismiss = useCallback(() => {
    setShowUpdatePasswordModal(false);
    setCurrentGroupForUpdate(null);

    if (showManageSecurityModal) {
      return;
    }

    setSecurityFilteredDeviceIds(undefined);
    resetAuthState();
    setCurrentAction(null);
    onActionComplete?.();
  }, [showManageSecurityModal, setCurrentAction, resetAuthState, onActionComplete]);

  return {
    showManageSecurityModal,
    showUpdatePasswordModal,
    hasThirdPartyMiners,
    minerGroups,
    startManageSecurity,
    handleSecurityAuthenticated,
    handleUpdateGroup,
    handleSecurityModalClose,
    handlePasswordConfirm,
    handlePasswordDismiss,
  };
};
