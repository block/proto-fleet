import { useCallback, useEffect, useState } from "react";

import type { DeviceCollection } from "@/protoFleet/api/generated/collection/v1/collection_pb";
import { useCollections } from "@/protoFleet/api/useCollections";
import GroupModal from "@/protoFleet/features/groupManagement/components/GroupModal";

import { Groups } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";

const GroupsPage = () => {
  const { listGroups } = useCollections();
  const [groups, setGroups] = useState<DeviceCollection[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [showGroupModal, setShowGroupModal] = useState(false);
  const [editGroup, setEditGroup] = useState<DeviceCollection | null>(null);

  const fetchGroups = useCallback(() => {
    setIsLoading(true);
    listGroups({
      onSuccess: (collections) => {
        setGroups(collections);
      },
      onFinally: () => {
        setIsLoading(false);
      },
    });
  }, [listGroups]);

  /* eslint-disable react-hooks/set-state-in-effect */
  useEffect(() => {
    fetchGroups();
  }, [fetchGroups]);
  /* eslint-enable react-hooks/set-state-in-effect */

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  const hasGroups = groups.length > 0;

  return (
    <div className="h-full">
      {!hasGroups ? (
        <div className="h-full p-6 sm:p-10">
          <div className="flex h-full w-full flex-col justify-center rounded-xl bg-surface-5 px-6 py-10 sm:px-20 sm:py-10 dark:bg-surface-base">
            <div className="flex flex-col gap-6">
              <div className="flex flex-col gap-4">
                <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-core-primary-5">
                  <Groups width="w-5" />
                </div>
                <Header title="Groups" titleSize="text-display-200" description="Organize your miners into groups." />
              </div>
              <div>
                <Button variant="primary" onClick={() => setShowGroupModal(true)}>
                  Add group
                </Button>
              </div>
            </div>
          </div>
        </div>
      ) : (
        <div className="p-10 phone:p-6 tablet:p-6">
          <div className="mb-6 flex items-center justify-between">
            <h1 className="text-heading-300 text-text-primary">Groups</h1>
            <Button variant="primary" onClick={() => setShowGroupModal(true)}>
              Add group
            </Button>
          </div>
          <div className="flex flex-col gap-3">
            {groups.map((group) => (
              <button
                type="button"
                key={String(group.id)}
                className="flex cursor-pointer items-center justify-between rounded-xl border border-border-10 p-4 text-left transition-colors hover:bg-surface-5"
                onClick={() => setEditGroup(group)}
              >
                <div>
                  <div className="text-emphasis-300 text-text-primary">{group.label}</div>
                  {group.description && <div className="text-200 text-text-primary-70">{group.description}</div>}
                </div>
                <div className="text-200 text-text-primary-70">
                  {group.deviceCount} {group.deviceCount === 1 ? "miner" : "miners"}
                </div>
              </button>
            ))}
          </div>
        </div>
      )}

      {showGroupModal && <GroupModal onDismiss={() => setShowGroupModal(false)} onSuccess={fetchGroups} />}

      {editGroup && <GroupModal group={editGroup} onDismiss={() => setEditGroup(null)} onSuccess={fetchGroups} />}
    </div>
  );
};

export default GroupsPage;
