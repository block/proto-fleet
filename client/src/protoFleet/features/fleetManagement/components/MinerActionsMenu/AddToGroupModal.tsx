import { ChangeEvent, useCallback, useEffect, useState } from "react";

import { type DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import { type SelectionMode } from "@/shared/components/List";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { pushToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";

interface AddToGroupModalProps {
  open?: boolean;
  onDismiss: () => void;
  selectedMiners: string[];
  selectionMode: SelectionMode;
  displayCount: number;
}

const pluralizeMiners = (count: number) => `${count} ${count === 1 ? "miner" : "miners"}`;

const AddToGroupModal = ({ open, onDismiss, selectedMiners, selectionMode, displayCount }: AddToGroupModalProps) => {
  const { createGroup, addDevicesToDeviceSet, listGroups } = useDeviceSets();

  const [groups, setGroups] = useState<DeviceSet[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [newGroupName, setNewGroupName] = useState("");
  const [selectedGroupIds, setSelectedGroupIds] = useState<Set<bigint>>(new Set());
  const [createNewChecked, setCreateNewChecked] = useState(false);

  useEffect(() => {
    if (!open) return;

    // eslint-disable-next-line react-hooks/set-state-in-effect -- reset modal state and fetch groups when modal opens
    setLoading(true);
    setGroups([]);
    setNewGroupName("");
    setSelectedGroupIds(new Set());
    setCreateNewChecked(false);

    listGroups({
      onSuccess: (deviceSets) => setGroups(deviceSets),
      onError: (message) => pushToast({ status: TOAST_STATUSES.error, message }),
      onFinally: () => setLoading(false),
    });
  }, [open, listGroups]);

  const allDevices = selectionMode === "all";
  const deviceIdentifiers = allDevices ? undefined : selectedMiners;
  const minerCount = allDevices ? displayCount : selectedMiners.length;
  const hasGroups = groups.length > 0;

  const canSave = hasGroups
    ? selectedGroupIds.size > 0 || (createNewChecked && newGroupName.trim().length > 0)
    : newGroupName.trim().length > 0;

  const handleToggleGroup = useCallback((id: bigint) => {
    setSelectedGroupIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }, []);

  const handleCreateNewToggle = useCallback((e: ChangeEvent<HTMLInputElement>) => {
    setCreateNewChecked(e.target.checked);
    if (!e.target.checked) {
      setNewGroupName("");
    }
  }, []);

  const handleSave = useCallback(async () => {
    if (!canSave) return;
    setSaving(true);

    const promises: Promise<void>[] = [];

    for (const groupId of selectedGroupIds) {
      promises.push(
        new Promise<void>((resolve, reject) => {
          addDevicesToDeviceSet({
            deviceSetId: groupId,
            deviceIdentifiers,
            allDevices,
            onSuccess: () => resolve(),
            onError: (msg) => reject(new Error(msg)),
          });
        }),
      );
    }

    const shouldCreateNew = hasGroups
      ? createNewChecked && newGroupName.trim().length > 0
      : newGroupName.trim().length > 0;

    if (shouldCreateNew) {
      promises.push(
        new Promise<void>((resolve, reject) => {
          createGroup({
            label: newGroupName.trim(),
            deviceIdentifiers,
            allDevices,
            onSuccess: () => resolve(),
            onError: (msg) => reject(new Error(msg)),
          });
        }),
      );
    }

    try {
      await Promise.all(promises);
      pushToast({
        status: TOAST_STATUSES.success,
        message: `Added ${pluralizeMiners(minerCount)} to group`,
      });
      onDismiss();
    } catch (err) {
      pushToast({ status: TOAST_STATUSES.error, message: getErrorMessage(err, "Failed to add to group") });
    } finally {
      setSaving(false);
    }
  }, [
    canSave,
    selectedGroupIds,
    hasGroups,
    createNewChecked,
    newGroupName,
    addDevicesToDeviceSet,
    createGroup,
    deviceIdentifiers,
    allDevices,
    minerCount,
    onDismiss,
  ]);

  if (!open) return null;

  const title = hasGroups ? "Add to group" : "Add group";
  const description = hasGroups
    ? `${pluralizeMiners(minerCount)} will be added to selected groups.`
    : `${pluralizeMiners(minerCount)} will be added to the group.`;

  return (
    <Modal
      open={open}
      onDismiss={onDismiss}
      title={title}
      description={description}
      divider={false}
      buttons={[
        {
          text: "Save",
          onClick: handleSave,
          disabled: !canSave || saving,
          loading: saving,
          variant: "primary",
          dismissModalOnClick: false,
        },
      ]}
    >
      {loading ? (
        <div className="flex justify-center py-20">
          <ProgressCircular indeterminate />
        </div>
      ) : hasGroups ? (
        <div>
          <label className="mb-6 flex items-center gap-6">
            <Checkbox checked={createNewChecked} onChange={handleCreateNewToggle} />
            <div className="flex-1">
              <Input
                id="new-group-name"
                label="New group name"
                initValue={newGroupName}
                onChange={(value) => setNewGroupName(value)}
                disabled={!createNewChecked}
              />
            </div>
          </label>

          {groups.map((group) => (
            <label
              key={group.id.toString()}
              className="flex cursor-pointer items-center gap-6 border-b border-border-10 py-3"
            >
              <Checkbox checked={selectedGroupIds.has(group.id)} onChange={() => handleToggleGroup(group.id)} />
              <span className="w-1/2 text-emphasis-300 text-text-primary">{group.label}</span>
              <span className="text-text-secondary w-1/2 text-emphasis-300">{pluralizeMiners(group.deviceCount)}</span>
            </label>
          ))}
        </div>
      ) : (
        <div className="mb-4">
          <Input
            id="new-group-name"
            label="Group name"
            initValue={newGroupName}
            onChange={(value) => setNewGroupName(value)}
            autoFocus
          />
        </div>
      )}
    </Modal>
  );
};

export default AddToGroupModal;
