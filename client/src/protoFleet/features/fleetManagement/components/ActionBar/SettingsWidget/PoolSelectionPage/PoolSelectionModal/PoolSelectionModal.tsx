import { useState } from "react";
import { MiningPool } from "../types";
import { sizes, variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Radio from "@/shared/components/Radio";

interface PoolSelectionModalProps {
  availablePools: MiningPool[];
  onDismiss: () => void;
  onSave: (selectedPoolId: string) => void;
}

const PoolSelectionModal = ({ availablePools, onDismiss, onSave }: PoolSelectionModalProps) => {
  const [selectedPoolId, setSelectedPoolId] = useState<string | undefined>();
  const [searchQuery, setSearchQuery] = useState("");

  const filteredPools = availablePools.filter((pool) => {
    const query = searchQuery.toLowerCase();
    return (
      pool.name.toLowerCase().includes(query) ||
      pool.poolUrl.toLowerCase().includes(query) ||
      pool.username.toLowerCase().includes(query)
    );
  });

  const handleSave = () => {
    if (selectedPoolId) {
      onSave(selectedPoolId);
      onDismiss();
    }
  };

  return (
    <Modal
      title="Select pool"
      showHeader
      divider
      buttonSize={sizes.base}
      buttons={[
        {
          text: "Save",
          variant: variants.primary,
          onClick: handleSave,
          dismissModalOnClick: false,
          disabled: !selectedPoolId,
        },
      ]}
      onDismiss={onDismiss}
      size="extraLarge"
    >
      <div className="mt-6 flex flex-col gap-6">
        <div className="w-[320px]">
          <Input
            id="pool-search"
            label="Search"
            initValue={searchQuery}
            onChange={(value) => setSearchQuery(value)}
            dismiss
            testId="pool-search-input"
            className="h-12"
          />
        </div>

        <div className="flex flex-col">
          <div className="flex items-center gap-4 border-b border-border-10 py-2">
            <div className="w-11"></div>
            <div className="flex-1 text-emphasis-300 text-text-primary-50">Name</div>
            <div className="flex-[2] text-emphasis-300 text-text-primary-50">URL</div>
            <div className="flex-1 text-emphasis-300 text-text-primary-50">Username</div>
          </div>

          <div className="flex max-h-[500px] flex-col overflow-y-auto">
            {filteredPools.length === 0 ? (
              <div className="text-text-secondary py-8 text-center text-300">No pools found</div>
            ) : (
              filteredPools.map((pool) => {
                const isSelected = selectedPoolId === pool.poolId;
                return (
                  <div
                    key={pool.poolId}
                    className="flex cursor-pointer items-center gap-4 border-b border-border-5 py-3 transition-colors hover:bg-gray-50 dark:hover:bg-gray-700/50"
                    onClick={() => setSelectedPoolId(pool.poolId)}
                  >
                    <div className="flex w-11 items-center justify-center">
                      <Radio selected={isSelected} />
                    </div>
                    <div className="flex flex-1 items-center truncate text-300 text-text-primary">{pool.name}</div>
                    <div className="flex flex-[2] items-center truncate text-300 text-text-primary">{pool.poolUrl}</div>
                    <div className="flex flex-1 items-center truncate text-300 text-text-primary">{pool.username}</div>
                  </div>
                );
              })
            )}
          </div>
        </div>
      </div>
    </Modal>
  );
};

export default PoolSelectionModal;
