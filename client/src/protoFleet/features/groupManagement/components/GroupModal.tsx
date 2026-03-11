import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import type { DeviceCollection } from "@/protoFleet/api/generated/collection/v1/collection_pb";
import type { MinerStateSnapshot as ProtoMinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { MinerListFilterSchema, PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useCollections } from "@/protoFleet/api/useCollections";
import useFleet from "@/protoFleet/api/useFleet";
import { INACTIVE_PLACEHOLDER } from "@/protoFleet/features/fleetManagement/components/MinerList/constants";

import { ChevronDown } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import { ActiveFilters, FilterItem } from "@/shared/components/List/Filters/types";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal, { ModalSelectAllFooter } from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { pushToast, STATUSES } from "@/shared/features/toaster";

interface GroupModalProps {
  onDismiss: () => void;
  onSuccess: () => void;
  group?: DeviceCollection;
}

type DeviceListItem = {
  deviceIdentifier: string;
  name: string;
  model: string;
  ipAddress: string;
  rackLabel: string;
  groupLabels: string[];
};

const modalCols = {
  name: "name",
  type: "type",
  rack: "rack",
  ipAddress: "ipAddress",
  group: "group",
} as const;

type ModalColumn = (typeof modalCols)[keyof typeof modalCols];

const modalColTitles: ColTitles<ModalColumn> = {
  name: "Name",
  type: "Type",
  rack: "Rack",
  ipAddress: "IP address",
  group: "Group",
};

const activeCols: ModalColumn[] = [
  modalCols.name,
  modalCols.type,
  modalCols.rack,
  modalCols.ipAddress,
  modalCols.group,
];

const modalColConfig: ColConfig<DeviceListItem, string, ModalColumn> = {
  [modalCols.name]: {
    component: (device: DeviceListItem) => <span>{device.name || device.deviceIdentifier}</span>,
    width: "min-w-28",
  },
  [modalCols.type]: {
    component: (device: DeviceListItem) => <span>{device.model || INACTIVE_PLACEHOLDER}</span>,
    width: "min-w-20",
  },
  [modalCols.rack]: {
    component: (device: DeviceListItem) => <span>{device.rackLabel || INACTIVE_PLACEHOLDER}</span>,
    width: "min-w-28",
  },
  [modalCols.ipAddress]: {
    component: (device: DeviceListItem) => <span>{device.ipAddress || INACTIVE_PLACEHOLDER}</span>,
    width: "min-w-24",
  },
  [modalCols.group]: {
    component: (device: DeviceListItem) => (
      <span>{device.groupLabels.length > 0 ? device.groupLabels.join(", ") : INACTIVE_PLACEHOLDER}</span>
    ),
    width: "min-w-24",
  },
};

const PAGE_SIZE = 50;

const toDeviceListItem = (miner: ProtoMinerStateSnapshot): DeviceListItem => ({
  deviceIdentifier: miner.deviceIdentifier,
  name: miner.name,
  model: miner.model,
  ipAddress: miner.ipAddress,
  rackLabel: miner.rackLabel,
  groupLabels: miner.groupLabels,
});

const GroupModal = ({ onDismiss, onSuccess, group }: GroupModalProps) => {
  const isEditMode = Boolean(group);
  const { createGroup, updateGroup, deleteGroup, listGroups, listRacks, listGroupMembers } = useCollections();
  const [groupName, setGroupName] = useState(group?.label ?? "");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const [errorMsg, setErrorMsg] = useState("");
  const [filter, setFilter] = useState(() => create(MinerListFilterSchema, {}));
  const [selectedItems, setSelectedItems] = useState<string[]>([]);
  const [allSelected, setAllSelected] = useState(false);
  const [isMembersLoading, setIsMembersLoading] = useState(isEditMode);
  const [availableGroups, setAvailableGroups] = useState<DeviceCollection[]>([]);
  const [availableRacks, setAvailableRacks] = useState<DeviceCollection[]>([]);

  const {
    minerIds,
    miners,
    totalMiners,
    isLoading,
    hasMore,
    currentPage,
    hasPreviousPage,
    goToNextPage,
    goToPrevPage,
    availableModels,
  } = useFleet({
    scope: "local",
    filter,
    pageSize: PAGE_SIZE,
    pairingStatuses: [PairingStatus.PAIRED],
  });

  const currentPageItems = useMemo(() => {
    if (!miners) return [];
    return minerIds
      .map((id) => miners[id])
      .filter((snapshot): snapshot is ProtoMinerStateSnapshot => Boolean(snapshot))
      .map(toDeviceListItem);
  }, [minerIds, miners]);

  const scrollRef = useRef<HTMLDivElement>(null);
  const currentPageItemsRef = useRef(currentPageItems);
  useEffect(() => {
    currentPageItemsRef.current = currentPageItems;
  }, [currentPageItems]);

  const scrollToTop = useCallback(() => {
    scrollRef.current?.scrollTo({ top: 0, behavior: "smooth" });
  }, []);

  const handleSetSelectedItems = useCallback((newSelection: string[]) => {
    setAllSelected(false);
    setSelectedItems((prev) => {
      const currentPageKeys = new Set(currentPageItemsRef.current.map((d) => d.deviceIdentifier));
      const offPageSelections = prev.filter((id) => !currentPageKeys.has(id));
      return [...offPageSelections, ...newSelection.filter((id) => currentPageKeys.has(id))];
    });
  }, []);

  const handleNextPage = useCallback(() => {
    scrollToTop();
    goToNextPage();
  }, [scrollToTop, goToNextPage]);

  const handlePrevPage = useCallback(() => {
    scrollToTop();
    goToPrevPage();
  }, [scrollToTop, goToPrevPage]);

  useEffect(() => {
    listGroups({ onSuccess: setAvailableGroups });
    listRacks({ onSuccess: setAvailableRacks });
  }, [listGroups, listRacks]);

  // Pre-select existing members in edit mode
  useEffect(() => {
    if (!group) return;
    listGroupMembers({
      collectionId: group.id,
      onSuccess: (identifiers) => {
        setSelectedItems(identifiers);
      },
      onError: (error) => {
        setErrorMsg(error || "Failed to load group members. Please close and try again.");
      },
      onFinally: () => {
        setIsMembersLoading(false);
      },
    });
  }, [group, listGroupMembers]);

  const filters = useMemo(
    (): FilterItem[] => [
      {
        type: "dropdown",
        title: "Type",
        value: "type",
        options: availableModels.map((model) => ({ id: model, label: model })),
        defaultOptionIds: [],
      },
      {
        type: "dropdown",
        title: "Rack",
        value: "rack",
        options: availableRacks.map((rack) => ({ id: String(rack.id), label: rack.label })),
        defaultOptionIds: [],
      },
      {
        type: "dropdown",
        title: "Group",
        value: "group",
        options: availableGroups.map((group) => ({ id: String(group.id), label: group.label })),
        defaultOptionIds: [],
      },
    ],
    [availableModels, availableRacks, availableGroups],
  );

  const handleServerFilter = useCallback(async (activeFilters: ActiveFilters) => {
    const minerFilter = create(MinerListFilterSchema, {
      errorComponentTypes: [],
    });

    const typeFilters = activeFilters.dropdownFilters.type;
    if (typeFilters && typeFilters.length > 0) {
      minerFilter.models.push(...typeFilters);
    }

    const rackFilters = activeFilters.dropdownFilters.rack;
    if (rackFilters && rackFilters.length > 0) {
      minerFilter.rackIds.push(...rackFilters.map((id) => BigInt(id)));
    }

    const groupFilters = activeFilters.dropdownFilters.group;
    if (groupFilters && groupFilters.length > 0) {
      minerFilter.groupIds.push(...groupFilters.map((id) => BigInt(id)));
    }

    setFilter(minerFilter);
  }, []);

  const handleSave = useCallback(() => {
    if (!groupName.trim()) {
      setErrorMsg("Group name is required");
      return;
    }

    if (!allSelected && selectedItems.length === 0) {
      setErrorMsg("Select at least one miner");
      return;
    }

    setIsSubmitting(true);
    setErrorMsg("");

    if (isEditMode && group) {
      updateGroup({
        collectionId: group.id,
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
  }, [groupName, selectedItems, allSelected, isEditMode, group, createGroup, updateGroup, onSuccess, onDismiss]);

  const handleDelete = useCallback(() => {
    if (!group) return;

    setIsDeleting(true);
    deleteGroup({
      collectionId: group.id,
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

  const showSpinner = (isLoading || isMembersLoading) && currentPageItems.length === 0;

  const modalButtons = useMemo(() => {
    const buttons = [];

    if (isEditMode) {
      buttons.push({
        text: "Delete group",
        onClick: () => setShowDeleteConfirm(true),
        variant: variants.secondaryDanger,
        dismissModalOnClick: false,
      });
    }

    buttons.push({
      text: "Save",
      onClick: handleSave,
      variant: variants.primary,
      loading: isSubmitting,
      disabled: isMembersLoading,
      dismissModalOnClick: false,
    });

    return buttons;
  }, [isEditMode, handleSave, isSubmitting, isMembersLoading]);

  return (
    <>
      <Modal
        onDismiss={onDismiss}
        open={!showDeleteConfirm}
        size="extraLarge"
        title={isEditMode ? "Edit group" : "Add group"}
        description={
          isEditMode ? "Rename your group or update its miners." : "Name your group and assign miners to it."
        }
        buttons={modalButtons}
        divider={false}
      >
        <div>
          {errorMsg ? (
            <div className="mb-4 rounded-lg bg-intent-critical-10 px-3 py-2 text-emphasis-300 text-intent-critical-text">
              {errorMsg}
            </div>
          ) : null}

          <div className="mb-4">
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

          {showSpinner ? (
            <div className="flex justify-center py-20">
              <ProgressCircular indeterminate />
            </div>
          ) : (
            <>
              <List<DeviceListItem, string, ModalColumn>
                activeCols={activeCols}
                colTitles={modalColTitles}
                colConfig={modalColConfig}
                filters={filters}
                onServerFilter={handleServerFilter}
                items={currentPageItems}
                itemKey="deviceIdentifier"
                itemSelectable
                customSelectedItems={selectedItems}
                customSetSelectedItems={handleSetSelectedItems}
                preserveOffPageSelection
                total={totalMiners}
                hideTotal
                itemName={{ singular: "miner", plural: "miners" }}
                containerClassName="max-h-[50vh]"
                overflowContainer
                stickyBgColor="bg-surface-elevated-base"
                scrollRef={scrollRef}
                footerContent={
                  !isLoading &&
                  totalMiners !== undefined &&
                  totalMiners > 0 && (
                    <div className="flex flex-col items-center gap-4 py-6">
                      <span className="text-300 text-text-primary">
                        Showing {currentPage * PAGE_SIZE + 1}–{currentPage * PAGE_SIZE + currentPageItems.length} of{" "}
                        {totalMiners} miners
                      </span>
                      <div className="flex gap-3">
                        <Button
                          variant={variants.secondary}
                          size={sizes.compact}
                          ariaLabel="Previous page"
                          prefixIcon={<ChevronDown className="rotate-90" />}
                          onClick={handlePrevPage}
                          disabled={!hasPreviousPage}
                        />
                        <Button
                          variant={variants.secondary}
                          size={sizes.compact}
                          ariaLabel="Next page"
                          prefixIcon={<ChevronDown className="rotate-270" />}
                          onClick={handleNextPage}
                          disabled={!hasMore}
                        />
                      </div>
                    </div>
                  )
                }
              />
              {totalMiners !== undefined && (
                <ModalSelectAllFooter
                  label={allSelected ? `All ${totalMiners} miners selected` : `${selectedItems.length} miners selected`}
                  onSelectAll={() => {
                    setAllSelected(true);
                    setSelectedItems(currentPageItems.map((d) => d.deviceIdentifier));
                  }}
                  onSelectNone={() => {
                    setAllSelected(false);
                    setSelectedItems([]);
                  }}
                />
              )}
            </>
          )}
        </div>
      </Modal>

      {showDeleteConfirm && group && (
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
      )}
    </>
  );
};

export default GroupModal;
