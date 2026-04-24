import { useCallback, useEffect, useRef, useState } from "react";

import type { DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import MinerSelectionList, { type MinerSelectionListHandle } from "@/protoFleet/components/MinerSelectionList";

import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Dialog from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import { pushToast, STATUSES } from "@/shared/features/toaster";

interface GroupModalProps {
  show: boolean;
  onDismiss: () => void;
  onSuccess: () => void;
  group?: DeviceSet;
}

const GroupModal = ({ show, onDismiss, onSuccess, group }: GroupModalProps) => {
  const isEditMode = Boolean(group);
  const { createGroup, updateGroup, deleteGroup, listGroupMembers } = useDeviceSets();
  const [groupName, setGroupName] = useState(group?.label ?? "");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const [errorMsg, setErrorMsg] = useState("");
  const [isMembersLoading, setIsMembersLoading] = useState(isEditMode);
  const [existingMemberIds, setExistingMemberIds] = useState<string[]>([]);

  const selectionRef = useRef<MinerSelectionListHandle>(null);

  // Pre-load existing members in edit mode
  useEffect(() => {
    if (!group) return;
    listGroupMembers({
      deviceSetId: group.id,
      onSuccess: (identifiers) => {
        setExistingMemberIds(identifiers);
      },
      onError: (error) => {
        setErrorMsg(error || "Failed to load group members. Please close and try again.");
      },
      onFinally: () => {
        setIsMembersLoading(false);
      },
    });
  }, [group, listGroupMembers]);

  const handleSave = useCallback(
    (selection: { selectedItems: string[]; allSelected: boolean }) => {
      const { selectedItems, allSelected } = selection;

      setIsSubmitting(true);
      setErrorMsg("");

      if (isEditMode && group) {
        updateGroup({
          deviceSetId: group.id,
          label: groupName.trim(),
          ...(allSelected ? { allDevices: true } : { deviceIdentifiers: selectedItems }),
          onSuccess: () => {
            pushToast({
              message: `Group "${groupName.trim()}" updated`,
              status: STATUSES.success,
            });
            onSuccess();
            onDismiss();
          },
          onError: (error) => {
            setErrorMsg(error || "Failed to update group. Please try again.");
          },
          onFinally: () => {
            setIsSubmitting(false);
          },
        });
      } else {
        createGroup({
          label: groupName.trim(),
          ...(allSelected ? { allDevices: true } : { deviceIdentifiers: selectedItems }),
          onSuccess: () => {
            pushToast({
              message: `Group "${groupName.trim()}" created`,
              status: STATUSES.success,
            });
            onSuccess();
            onDismiss();
          },
          onError: (error) => {
            setErrorMsg(error || "Failed to create group. Please try again.");
          },
          onFinally: () => {
            setIsSubmitting(false);
          },
        });
      }
    },
    [groupName, isEditMode, group, createGroup, updateGroup, onSuccess, onDismiss],
  );

  const handleDelete = useCallback(() => {
    if (!group) return;

    setIsDeleting(true);
    deleteGroup({
      deviceSetId: group.id,
      onSuccess: () => {
        pushToast({
          message: `Group "${group.label}" deleted`,
          status: STATUSES.success,
        });
        onSuccess();
        onDismiss();
      },
      onError: (error) => {
        setShowDeleteConfirm(false);
        setErrorMsg(error || "Failed to delete group. Please try again.");
      },
      onFinally: () => {
        setIsDeleting(false);
      },
    });
  }, [group, deleteGroup, onSuccess, onDismiss]);

  const handleSaveClick = useCallback(() => {
    if (!groupName.trim()) {
      setErrorMsg("Group name is required");
      return;
    }
    const selection = selectionRef.current?.getSelection();
    if (!selection) return;
    const { selectedItems, allSelected } = selection;
    if (!allSelected && selectedItems.length === 0) {
      setErrorMsg("Select at least one miner");
      return;
    }
    handleSave({ selectedItems, allSelected });
  }, [groupName, handleSave]);

  if (show === false) return null;

  return (
    <>
      <Modal
        onDismiss={onDismiss}
        open={show ? !showDeleteConfirm : false}
        size="large"
        className="flex !h-[calc(100vh-(--spacing(32)))] max-h-[calc(100vh-(--spacing(32)))] flex-col !overflow-hidden"
        bodyClassName="flex flex-1 min-h-0 flex-col"
        buttons={[
          ...(isEditMode
            ? [
                {
                  text: "Delete group",
                  onClick: () => setShowDeleteConfirm(true),
                  variant: variants.secondaryDanger,
                  dismissModalOnClick: false,
                },
              ]
            : []),
          {
            text: "Save",
            onClick: handleSaveClick,
            variant: variants.primary,
            loading: isSubmitting,
            disabled: isMembersLoading,
            dismissModalOnClick: false,
          },
        ]}
        divider={false}
        title={isEditMode ? "Edit group" : "Add group"}
        description={
          isEditMode ? "Rename your group or update its miners." : "Name your group and assign miners to it."
        }
      >
        <div className="mt-4 flex h-full min-h-0 flex-col">
          {errorMsg ? (
            <Callout
              className="mb-4 shrink-0"
              intent="danger"
              prefixIcon={<Alert />}
              testId="error-msg"
              title={errorMsg}
            />
          ) : null}

          <div className="mb-4 shrink-0">
            <Input
              id="group-name"
              label="Group name"
              initValue={groupName}
              onChange={(value) => {
                setGroupName(value);
                setErrorMsg("");
              }}
            />
          </div>

          <MinerSelectionList
            ref={selectionRef}
            filterConfig={{ showTypeFilter: true, showRackFilter: true, showGroupFilter: true }}
            initialSelectedItems={existingMemberIds}
            isMembersLoading={isMembersLoading}
          />
        </div>
      </Modal>

      {showDeleteConfirm && group ? (
        <Dialog
          title={`Delete "${group.label}"?`}
          subtitle="This action cannot be undone. The miners in this group will not be affected."
          onDismiss={() => setShowDeleteConfirm(false)}
          buttons={[
            {
              text: "Cancel",
              onClick: () => setShowDeleteConfirm(false),
              variant: variants.secondary,
            },
            {
              text: "Delete",
              onClick: handleDelete,
              variant: variants.danger,
              loading: isDeleting,
            },
          ]}
        />
      ) : null}
    </>
  );
};

export default GroupModal;
