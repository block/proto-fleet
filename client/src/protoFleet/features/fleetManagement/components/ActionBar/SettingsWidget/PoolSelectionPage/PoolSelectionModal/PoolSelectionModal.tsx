import { useState } from "react";
import { create } from "@bufbuild/protobuf";
import { MiningPool } from "../types";
import { CreatePoolRequestSchema } from "@/protoFleet/api/generated/pools/v1/pools_pb";
import usePools from "@/protoFleet/api/usePools";
import { sizes, variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import { emptyPoolInfo } from "@/shared/components/MiningPools/constants";
import PoolModal from "@/shared/components/MiningPools/PoolModal";
import { PoolInfo } from "@/shared/components/MiningPools/types";
import Modal from "@/shared/components/Modal";
import Radio from "@/shared/components/Radio";

interface PoolSelectionModalProps {
  onDismiss: () => void;
  onSave: (selectedPoolId: string, poolData?: MiningPool) => void;
  excludedPoolIds?: (string | undefined)[];
  poolAssignments?: Record<string, string>;
}

const PoolSelectionModal = ({
  onDismiss,
  onSave,
  excludedPoolIds = [],
  poolAssignments = {},
}: PoolSelectionModalProps) => {
  const [selectedPoolId, setSelectedPoolId] = useState<string | undefined>();
  const [searchQuery, setSearchQuery] = useState("");
  const [showAddPoolModal, setShowAddPoolModal] = useState(false);
  const [newPoolInfo, setNewPoolInfo] = useState<PoolInfo[]>([emptyPoolInfo]);
  const [isTestingConnection, setIsTestingConnection] = useState(false);

  const { validatePool, createPool, miningPools } = usePools();

  const filteredPools = miningPools.filter((pool) => {
    const query = searchQuery.toLowerCase();
    return (
      pool.name.toLowerCase().includes(query) ||
      pool.poolUrl.toLowerCase().includes(query) ||
      pool.username.toLowerCase().includes(query)
    );
  });

  const isPoolExcluded = (poolId: string) => excludedPoolIds.some((id) => id === poolId);

  const handleSave = () => {
    if (selectedPoolId) {
      onSave(selectedPoolId);
      onDismiss();
    }
  };

  const handleAddNewPool = () => {
    setShowAddPoolModal(true);
  };

  const handleNewPoolSave = async (pool: PoolInfo, isPasswordSet: boolean) => {
    const createPoolRequest = create(CreatePoolRequestSchema, {
      poolConfig: {
        poolName: pool.name || "",
        url: pool.url || "",
        username: pool.username || "",
        password: isPasswordSet && pool.password ? pool.password : "",
      },
    });

    return new Promise<void>((resolve, reject) => {
      createPool({
        createPoolRequest,
        onSuccess: (poolId) => {
          setShowAddPoolModal(false);

          const newPoolData: MiningPool = {
            poolId: poolId,
            name: pool.name || "",
            poolUrl: pool.url || "",
            username: pool.username || "",
          };

          onSave(poolId, newPoolData);
          resolve();
        },
        onError: (error) => {
          reject(new Error(error));
        },
      });
    });
  };

  const handlePoolModalDismiss = () => {
    setShowAddPoolModal(false);
    setNewPoolInfo([emptyPoolInfo]);
  };

  const handleTestConnection = (args: {
    poolInfo: PoolInfo;
    onError?: (error?: string) => void;
    onSuccess?: () => void;
    onFinally?: () => void;
  }) => {
    setIsTestingConnection(true);
    validatePool({
      poolInfo: {
        url: args.poolInfo.url,
        username: args.poolInfo.username,
        password: args.poolInfo.password,
      },
      onSuccess: () => {
        args.onSuccess?.();
      },
      onError: (error) => {
        args.onError?.(error);
      },
      onFinally: () => {
        setIsTestingConnection(false);
        args.onFinally?.();
      },
    });
  };

  if (showAddPoolModal) {
    return (
      <PoolModal
        onChangePools={setNewPoolInfo}
        onDismiss={handlePoolModalDismiss}
        poolIndex={0}
        pools={newPoolInfo}
        show={true}
        isTestingConnection={isTestingConnection}
        testConnection={handleTestConnection}
        onSave={handleNewPoolSave}
      />
    );
  }

  return (
    <Modal
      title="Select pool"
      showHeader
      divider
      buttonSize={sizes.base}
      buttons={[
        {
          text: "Add new pool",
          variant: variants.secondary,
          onClick: handleAddNewPool,
          dismissModalOnClick: false,
        },
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
            <div className="w-28 text-emphasis-300 text-text-primary-50">Assigned to</div>
          </div>

          <div className="flex max-h-[500px] flex-col overflow-y-auto">
            {filteredPools.length === 0 ? (
              <div className="text-text-secondary py-8 text-center text-300">No pools found</div>
            ) : (
              filteredPools.map((pool) => {
                const isSelected = selectedPoolId === pool.poolId;
                const isExcluded = isPoolExcluded(pool.poolId);
                const assignmentLabel = poolAssignments[pool.poolId];

                return (
                  <div
                    key={pool.poolId}
                    className={`flex items-center gap-4 border-b border-border-5 py-3 transition-colors ${
                      isExcluded
                        ? "cursor-not-allowed opacity-50"
                        : "cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-700/50"
                    }`}
                    onClick={() => !isExcluded && setSelectedPoolId(pool.poolId)}
                    data-testid={`pool-row-${pool.name}`}
                    aria-disabled={isExcluded}
                  >
                    <div className="flex w-11 items-center justify-center">
                      <Radio selected={isSelected} disabled={isExcluded} />
                    </div>
                    <div
                      className="flex flex-1 items-center truncate text-300 text-text-primary"
                      data-testid="pool-name"
                    >
                      {pool.name}
                    </div>
                    <div
                      className="flex flex-[2] items-center truncate text-300 text-text-primary"
                      data-testid="pool-url"
                    >
                      {pool.poolUrl}
                    </div>
                    <div
                      className="flex flex-1 items-center truncate text-300 text-text-primary"
                      data-testid="pool-username"
                    >
                      {pool.username}
                    </div>
                    <div className="w-28 text-300" data-testid="pool-assignment">
                      {assignmentLabel ? (
                        <span className="text-text-secondary rounded bg-surface-5 px-2 py-0.5 text-200 whitespace-nowrap">
                          {assignmentLabel}
                        </span>
                      ) : (
                        <span className="text-text-tertiary">—</span>
                      )}
                    </div>
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
