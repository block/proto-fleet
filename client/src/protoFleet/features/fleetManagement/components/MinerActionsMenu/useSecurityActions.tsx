import { useCallback, useState } from "react";
import { loadingMessages, minersMessage, settingsActions, type SupportedAction } from "./constants";
import { type MinerGroup } from "./ManageSecurity";
import {
  type DeviceSelector,
  type UpdateMinerPasswordResponse,
} from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import { minerTypes } from "@/protoFleet/features/fleetManagement/components/MinerList/constants";
import { createDeviceSelector } from "@/protoFleet/features/fleetManagement/utils/deviceSelector";
import { useFleetStore, useStartBatchOperation } from "@/protoFleet/store";
import { pushToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";

type PendingActionCallback = (filteredSelector?: DeviceSelector, filteredDeviceIds?: string[]) => void;

export interface SecurityActionsProps {
  showAuthenticateFleetModal: boolean;
  authenticationPurpose: "security" | "pool" | null;
  showUpdatePasswordModal: boolean;
  hasProtoMiners: boolean;
  handleFleetAuthenticated: (username: string, password: string) => void;
  handlePasswordConfirm: (currentPassword: string, newPassword: string) => void;
  handlePasswordDismiss: () => void;
  handleAuthDismiss: () => void;
  showManageSecurityModal: boolean;
  minerGroups: MinerGroup[];
  handleUpdateGroup: (group: MinerGroup) => void;
  handleSecurityModalDone: () => void;
  handleSecurityModalDismiss: () => void;
}

export interface UseSecurityActionsParams {
  deviceIdentifiers: string[];
  fleetCredentials: { username: string; password: string } | undefined;
  clearFleetCredentials: () => void;
  onActionComplete?: () => void;
  handleSuccess: (
    action: SupportedAction,
    toastId: number,
    batchIdentifier: string,
    onBatchComplete?: (successIds: string[], failureIds: string[]) => void,
  ) => void;
  handleError: (toastId: number, error: string) => void;
  checkAndShowUnsupportedMinersModal: (
    action: SupportedAction,
    proceedAction: PendingActionCallback,
  ) => Promise<boolean>;
  setCurrentAction: (action: SupportedAction | null) => void;
}

function groupMinersByModel(deviceIds: string[]): MinerGroup[] {
  const miners = useFleetStore.getState().fleet.miners;
  const groupMap = new Map<string, MinerGroup>();

  deviceIds.forEach((id) => {
    const miner = miners[id];
    if (!miner) return;

    const manufacturer = miner.manufacturer?.toLowerCase() || "unknown";
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

export const useSecurityActions = ({
  deviceIdentifiers,
  fleetCredentials,
  clearFleetCredentials,
  onActionComplete,
  handleSuccess,
  handleError,
  checkAndShowUnsupportedMinersModal,
  setCurrentAction,
}: UseSecurityActionsParams) => {
  const { updateMinerPassword } = useMinerCommand();
  const startBatchOperation = useStartBatchOperation();

  const [showUpdatePasswordModal, setShowUpdatePasswordModal] = useState(false);
  const [hasProtoMiners, setHasProtoMiners] = useState(false);
  const [securityFilteredDeviceIds, setSecurityFilteredDeviceIds] = useState<string[] | undefined>(undefined);
  const [showManageSecurityModal, setShowManageSecurityModal] = useState(false);
  const [minerGroups, setMinerGroups] = useState<MinerGroup[]>([]);
  const [currentGroupForUpdate, setCurrentGroupForUpdate] = useState<MinerGroup | null>(null);

  const initSecurityFlow = useCallback(async () => {
    const modalShown = await checkAndShowUnsupportedMinersModal(
      settingsActions.security,
      (_filteredSelector, filteredDeviceIds) => {
        const deviceIdsToUse = filteredDeviceIds ?? deviceIdentifiers;
        setSecurityFilteredDeviceIds(filteredDeviceIds);
        setCurrentAction(settingsActions.security);
        setMinerGroups(groupMinersByModel(deviceIdsToUse));
        setShowManageSecurityModal(true);
      },
    );

    if (!modalShown) {
      setSecurityFilteredDeviceIds(undefined);
      setMinerGroups(groupMinersByModel(deviceIdentifiers));
      setShowManageSecurityModal(true);
    }
  }, [checkAndShowUnsupportedMinersModal, deviceIdentifiers, setCurrentAction]);

  const resetState = useCallback(() => {
    setSecurityFilteredDeviceIds(undefined);
    setShowManageSecurityModal(false);
    setMinerGroups([]);
    setCurrentGroupForUpdate(null);
    setShowUpdatePasswordModal(false);
    setHasProtoMiners(false);
  }, []);

  const handleUpdateGroup = useCallback((group: MinerGroup) => {
    setCurrentGroupForUpdate(group);
    setHasProtoMiners(group.manufacturer === minerTypes.protoRig);
    setShowUpdatePasswordModal(true);
  }, []);

  const handleSecurityModalClose = useCallback(() => {
    setShowManageSecurityModal(false);
    setMinerGroups([]);
    clearFleetCredentials();
    setSecurityFilteredDeviceIds(undefined);
    setCurrentAction(null);
    onActionComplete?.();
  }, [clearFleetCredentials, onActionComplete, setCurrentAction]);

  const handlePasswordConfirm = useCallback(
    (currentPassword: string, newPassword: string) => {
      const deviceIdsToUse = currentGroupForUpdate
        ? currentGroupForUpdate.deviceIdentifiers
        : (securityFilteredDeviceIds ?? deviceIdentifiers);
      const selectorToUse = createDeviceSelector("subset", deviceIdsToUse);

      if (!fleetCredentials) return;

      setShowUpdatePasswordModal(false);

      const id = pushToast({
        message: `${loadingMessages[settingsActions.security]} ${minersMessage}`,
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

          if (!currentGroupForUpdate) {
            clearFleetCredentials();
            setSecurityFilteredDeviceIds(undefined);
          }
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
      currentGroupForUpdate,
      securityFilteredDeviceIds,
      deviceIdentifiers,
      fleetCredentials,
      updateMinerPassword,
      handleSuccess,
      handleError,
      onActionComplete,
      startBatchOperation,
      clearFleetCredentials,
      setCurrentAction,
    ],
  );

  const handlePasswordDismiss = useCallback(() => {
    setShowUpdatePasswordModal(false);
    setCurrentGroupForUpdate(null);

    if (showManageSecurityModal) {
      return;
    }

    setSecurityFilteredDeviceIds(undefined);
    clearFleetCredentials();
    setCurrentAction(null);
    onActionComplete?.();
  }, [showManageSecurityModal, clearFleetCredentials, onActionComplete, setCurrentAction]);

  return {
    showUpdatePasswordModal,
    hasProtoMiners,
    showManageSecurityModal,
    minerGroups,
    handleUpdateGroup,
    handleSecurityModalDone: handleSecurityModalClose,
    handleSecurityModalDismiss: handleSecurityModalClose,
    handlePasswordConfirm,
    handlePasswordDismiss,
    initSecurityFlow,
    resetState,
  };
};
