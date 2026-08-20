import { useCallback, useEffect, useMemo, useState } from "react";

import type { DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { useDeviceSets } from "@/protoFleet/api/useDeviceSets";
import type { SiteFilterFields } from "@/protoFleet/components/PageHeader/SitePicker";
import Checkbox from "@/shared/components/Checkbox";
import Modal from "@/shared/components/Modal";
import ModalSelectAllFooter from "@/shared/components/Modal/ModalSelectAllFooter";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";
import { pushToast, STATUSES } from "@/shared/features/toaster";

export interface GroupSelectionModalProps {
  open: boolean;
  selectedGroupIds: string[];
  scope?: SiteFilterFields;
  preserveMissingSelections?: boolean;
  onDismiss: () => void;
  onSave: (groupIds: string[]) => void;
}

const formatMinerCount = (count: number) => `${count} ${count === 1 ? "miner" : "miners"}`;

const GroupSelectionModal = ({
  open,
  selectedGroupIds,
  scope,
  preserveMissingSelections = false,
  onDismiss,
  onSave,
}: GroupSelectionModalProps) => {
  const { listGroups } = useDeviceSets();
  const [groups, setGroups] = useState<DeviceSet[]>([]);
  const [draftSelection, setDraftSelection] = useState<Set<string>>(new Set(selectedGroupIds));
  const [isLoading, setIsLoading] = useState(true);
  const [hasLoadError, setHasLoadError] = useState(false);
  const siteIds = scope?.siteIds;
  const includeUnassigned = scope?.includeUnassigned;
  const isScoped = (siteIds !== undefined && siteIds.length > 0) || includeUnassigned === true;

  useEffect(() => {
    listGroups({
      siteIds,
      includeUnassigned,
      onSuccess: (deviceSets) => {
        setGroups(deviceSets);

        if (isScoped || preserveMissingSelections) return;
        const validGroupIds = new Set(deviceSets.map((group) => group.id.toString()));
        setDraftSelection((current) => new Set([...current].filter((groupId) => validGroupIds.has(groupId))));
      },
      onError: (message: string) => {
        setHasLoadError(true);
        pushToast({
          message: message || "Failed to load groups",
          status: STATUSES.error,
        });
      },
      onFinally: () => setIsLoading(false),
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [listGroups, includeUnassigned, preserveMissingSelections, (siteIds ?? []).map(String).join(",")]);

  const selectedGroupCount = useMemo(
    () => groups.filter((group) => draftSelection.has(group.id.toString())).length,
    [draftSelection, groups],
  );
  const missingSelectedGroupIds = useMemo(() => {
    if (!preserveMissingSelections) return [];
    const availableIds = new Set(groups.map((group) => group.id.toString()));
    return [...draftSelection].filter((groupId) => !availableIds.has(groupId));
  }, [draftSelection, groups, preserveMissingSelections]);

  const allSelected = useMemo(
    () => groups.length > 0 && selectedGroupCount === groups.length,
    [selectedGroupCount, groups.length],
  );
  const hasGroups = groups.length > 0;
  const showEmptyState = !isLoading && !hasGroups && missingSelectedGroupIds.length === 0;

  const toggleGroup = useCallback((groupId: string) => {
    setDraftSelection((current) => {
      const next = new Set(current);

      if (next.has(groupId)) {
        next.delete(groupId);
      } else {
        next.add(groupId);
      }

      return next;
    });
  }, []);

  if (!open) {
    return null;
  }

  return (
    <Modal
      open={open}
      onDismiss={onDismiss}
      title={hasLoadError ? "Couldn't load groups" : showEmptyState ? "No groups configured" : "Select groups"}
      divider={false}
      buttons={[
        {
          text: "Done",
          variant: "primary",
          onClick: () => onSave(Array.from(draftSelection)),
          dismissModalOnClick: false,
          disabled: hasLoadError,
        },
      ]}
    >
      {isLoading ? (
        <div className="flex justify-center py-20">
          <ProgressCircular indeterminate />
        </div>
      ) : hasLoadError ? (
        <div className="text-300 text-text-primary-70">Couldn&apos;t load groups. Close this modal and try again.</div>
      ) : showEmptyState ? (
        <div className="text-300 text-text-primary-70">Create a group to target miners by group.</div>
      ) : (
        <div className="flex flex-col">
          {hasGroups ? (
            <Row divider>
              <label className="flex w-full cursor-pointer items-center gap-4">
                <Checkbox
                  checked={allSelected}
                  partiallyChecked={!allSelected ? selectedGroupCount > 0 : false}
                  onChange={() =>
                    setDraftSelection((current) =>
                      allSelected
                        ? new Set<string>()
                        : new Set([...current, ...groups.map((group) => group.id.toString())]),
                    )
                  }
                />
                <div className="flex flex-col">
                  <span className="text-emphasis-300 text-text-primary">All groups</span>
                </div>
              </label>
            </Row>
          ) : null}

          {groups.map((group) => (
            <Row key={group.id.toString()} divider={false} compact>
              <label className="flex w-full cursor-pointer items-center gap-4">
                <Checkbox
                  checked={draftSelection.has(group.id.toString())}
                  onChange={() => toggleGroup(group.id.toString())}
                />
                <div className="flex flex-col">
                  <span className="text-emphasis-300 text-text-primary">{group.label}</span>
                  <span className="text-200 text-text-primary-70">{formatMinerCount(group.deviceCount)}</span>
                </div>
              </label>
            </Row>
          ))}

          {missingSelectedGroupIds.map((groupId) => (
            <Row key={groupId} divider={false} compact>
              <label className="flex w-full cursor-pointer items-center gap-4">
                <Checkbox checked onChange={() => toggleGroup(groupId)} />
                <span className="text-emphasis-300 text-text-primary-70">Unavailable group ({groupId})</span>
              </label>
            </Row>
          ))}

          <ModalSelectAllFooter
            label={`${preserveMissingSelections ? draftSelection.size : selectedGroupCount} ${(preserveMissingSelections ? draftSelection.size : selectedGroupCount) === 1 ? "group" : "groups"} selected`}
            onSelectAll={() =>
              setDraftSelection((current) => new Set([...current, ...groups.map((group) => group.id.toString())]))
            }
            onSelectNone={() => setDraftSelection(new Set())}
          />
        </div>
      )}
    </Modal>
  );
};

export default GroupSelectionModal;
