import { useCallback, useEffect, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { MiningPool } from "../types";
import { CreatePoolRequestSchema, ValidationMode } from "@/protoFleet/api/generated/pools/v1/pools_pb";
import usePools from "@/protoFleet/api/usePools";
import { Alert, Success } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import { DismissibleCalloutWrapper, intents } from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import { emptyPoolInfo } from "@/shared/components/MiningPools/constants";
import { fleetUsernameHelperText } from "@/shared/components/MiningPools/PoolForm/constants";
import PoolModal from "@/shared/components/MiningPools/PoolModal";
import { PoolConnectionTestOutcome, PoolConnectionTestProps, PoolInfo } from "@/shared/components/MiningPools/types";
import Modal from "@/shared/components/Modal";
import Radio from "@/shared/components/Radio";

const filterPoolsByQuery = (pools: MiningPool[], query: string): MiningPool[] => {
  const lowerQuery = query.toLowerCase();
  return pools.filter(
    (pool) =>
      pool.name.toLowerCase().includes(lowerQuery) ||
      pool.poolUrl.toLowerCase().includes(lowerQuery) ||
      pool.username.toLowerCase().includes(lowerQuery),
  );
};

interface PoolSelectableRowProps {
  pool: MiningPool;
  isSelected: boolean;
  isDisabled: boolean;
  onSelect?: () => void;
  testId: string;
}

const PoolSelectableRow = ({ pool, isSelected, isDisabled, onSelect, testId }: PoolSelectableRowProps) => (
  <div
    className={`flex items-center gap-4 border-b border-border-5 py-3 transition-colors ${
      isDisabled ? "cursor-not-allowed opacity-50" : "cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-700/50"
    }`}
    onClick={() => !isDisabled && onSelect?.()}
    data-testid={testId}
    aria-disabled={isDisabled}
  >
    <div className="flex w-11 items-center justify-center">
      <Radio selected={isSelected} disabled={isDisabled} />
    </div>
    <div className="flex flex-1 items-center truncate text-300 text-text-primary" data-testid="pool-name">
      {pool.name}
    </div>
    <div className="flex flex-[2] items-center truncate text-300 text-text-primary" data-testid="pool-url">
      {pool.poolUrl}
    </div>
    <div className="flex flex-1 items-center truncate text-300 text-text-primary" data-testid="pool-username">
      {pool.username}
    </div>
  </div>
);

interface PoolSelectionModalProps {
  open?: boolean;
  onDismiss: () => void;
  onSave: (selectedPoolId: string, poolData?: MiningPool) => void;
  excludedPoolIds?: (string | undefined)[];
  unknownPools?: MiningPool[];
}

const PoolSelectionModal = ({
  open,
  onDismiss,
  onSave,
  excludedPoolIds = [],
  unknownPools = [],
}: PoolSelectionModalProps) => {
  const isVisible = open ?? true;
  const [selectedPoolId, setSelectedPoolId] = useState<string | undefined>();
  const [searchQuery, setSearchQuery] = useState("");
  const [showAddPoolModal, setShowAddPoolModal] = useState(false);
  const [newPoolInfo, setNewPoolInfo] = useState<PoolInfo[]>([emptyPoolInfo]);
  const [isTestingConnection, setIsTestingConnection] = useState(false);
  const [showConnectionCallout, setShowConnectionCallout] = useState(false);
  const [connectionError, setConnectionError] = useState(false);
  const [lastTestOutcome, setLastTestOutcome] = useState<PoolConnectionTestOutcome | undefined>();

  const { validatePool, createPool, miningPools } = usePools(isVisible);

  useEffect(() => {
    if (isVisible) {
      return;
    }

    // eslint-disable-next-line react-hooks/set-state-in-effect -- reset modal state on close to mirror prior conditional-mount behavior
    setSelectedPoolId(undefined);
    setSearchQuery("");
    setShowAddPoolModal(false);
    setNewPoolInfo([emptyPoolInfo]);
    setIsTestingConnection(false);
    setShowConnectionCallout(false);
    setConnectionError(false);
    setLastTestOutcome(undefined);
  }, [isVisible]);

  // Saved-pool tests don't carry the encrypted password back through
  // the client, so SV1 pools authenticate as reachable-but-unverified
  // and SV2 pools come back as TCP-dial-only. Reflect that in the
  // success callout so operators can't misread an unverified probe as
  // proof of working credentials.
  const showSuccessCallout = useMemo(
    () =>
      showConnectionCallout &&
      !isTestingConnection &&
      !connectionError &&
      lastTestOutcome !== undefined &&
      lastTestOutcome.reachable &&
      lastTestOutcome.credentialsVerified,
    [showConnectionCallout, isTestingConnection, connectionError, lastTestOutcome],
  );

  const showReachableUnverifiedCallout = useMemo(
    () =>
      showConnectionCallout &&
      !isTestingConnection &&
      !connectionError &&
      lastTestOutcome !== undefined &&
      lastTestOutcome.reachable &&
      !lastTestOutcome.credentialsVerified,
    [showConnectionCallout, isTestingConnection, connectionError, lastTestOutcome],
  );

  const reachableUnverifiedTitle = useMemo(() => {
    if (!lastTestOutcome) {
      return "";
    }
    switch (lastTestOutcome.mode) {
      case ValidationMode.SV2_TCP_DIAL:
        return "Pool reachable. Credentials not verified — Stratum V2 connectivity check completed a TCP dial only.";
      case ValidationMode.SV2_HANDSHAKE:
        return "Pool reachable and Noise handshake succeeded. Credentials are verified at job-submission time.";
      default:
        return "Pool reachable. Credentials not verified.";
    }
  }, [lastTestOutcome]);

  const showErrorCallout = useMemo(
    () => showConnectionCallout && !isTestingConnection && connectionError,
    [showConnectionCallout, isTestingConnection, connectionError],
  );

  const filteredPools = useMemo(() => filterPoolsByQuery(miningPools, searchQuery), [miningPools, searchQuery]);

  const filteredUnknownPools = useMemo(
    () => filterPoolsByQuery(unknownPools, searchQuery),
    [unknownPools, searchQuery],
  );

  const isPoolExcluded = (poolId: string) => excludedPoolIds.includes(poolId);

  const handleSave = () => {
    if (selectedPoolId) {
      onSave(selectedPoolId);
    }
  };

  const handleTestSelectedConnection = useCallback(() => {
    if (!selectedPoolId) return;

    const selectedPool = miningPools.find((p) => p.poolId === selectedPoolId);
    if (!selectedPool) return;

    setIsTestingConnection(true);
    setConnectionError(false);
    setLastTestOutcome(undefined);
    validatePool({
      poolInfo: {
        url: selectedPool.poolUrl,
        username: selectedPool.username,
      },
      onSuccess: (outcome) => {
        setConnectionError(false);
        setLastTestOutcome(outcome);
      },
      onError: () => {
        setConnectionError(true);
      },
      onFinally: () => {
        setIsTestingConnection(false);
        setShowConnectionCallout(true);
      },
    });
  }, [selectedPoolId, miningPools, validatePool]);

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

  const handleTestConnection = (args: PoolConnectionTestProps) => {
    setIsTestingConnection(true);
    validatePool({
      poolInfo: {
        url: args.poolInfo.url,
        username: args.poolInfo.username,
        password: args.poolInfo.password,
      },
      onSuccess: (outcome) => {
        args.onSuccess?.(outcome);
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
        open={open}
        onChangePools={setNewPoolInfo}
        onDismiss={handlePoolModalDismiss}
        poolIndex={0}
        pools={newPoolInfo}
        isTestingConnection={isTestingConnection}
        testConnection={handleTestConnection}
        onSave={handleNewPoolSave}
        usernameHelperText={fleetUsernameHelperText}
        disallowUsernameSeparator
      />
    );
  }

  return (
    <Modal
      open={open}
      title="Select pool"
      showHeader
      divider
      buttons={[
        {
          text: "Test connection",
          variant: variants.secondary,
          onClick: handleTestSelectedConnection,
          dismissModalOnClick: false,
          disabled: !selectedPoolId || isTestingConnection,
          loading: isTestingConnection,
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
      size="large"
    >
      <div className="mt-6 flex flex-col gap-6">
        <DismissibleCalloutWrapper
          icon={<Success />}
          intent={intents.success}
          onDismiss={() => setShowConnectionCallout(false)}
          show={showSuccessCallout}
          title="Pool connection successful"
          testId="pool-selection-modal-connection-success-callout"
        />
        <DismissibleCalloutWrapper
          icon={<Alert width={iconSizes.medium} />}
          intent={intents.warning}
          onDismiss={() => setShowConnectionCallout(false)}
          show={showReachableUnverifiedCallout}
          title={reachableUnverifiedTitle}
          testId="pool-selection-modal-connection-unverified-callout"
        />
        <DismissibleCalloutWrapper
          icon={<Alert width={iconSizes.medium} />}
          intent={intents.danger}
          onDismiss={() => setShowConnectionCallout(false)}
          show={showErrorCallout}
          title="We couldn't connect with your pool. Review your pool details and try again."
          testId="pool-selection-modal-connection-error-callout"
        />
        <div className="w-[320px]">
          <Input
            id="pool-search"
            label="Search"
            initValue={searchQuery}
            onChange={(value) => setSearchQuery(value)}
            dismiss
            testId="pool-search-input"
            className="h-12"
            autoFocus
          />
        </div>

        {/* Add new pool button */}
        <div className="flex">
          <Button
            text="Add new pool"
            variant={variants.secondary}
            size={sizes.base}
            onClick={() => setShowAddPoolModal(true)}
            testId="add-new-pool-button"
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
            {filteredPools.length === 0 && filteredUnknownPools.length === 0 && searchQuery ? (
              <div className="text-text-secondary py-8 text-center text-300">No pools found</div>
            ) : (
              <>
                {filteredPools.map((pool) => (
                  <PoolSelectableRow
                    key={pool.poolId}
                    pool={pool}
                    isSelected={selectedPoolId === pool.poolId}
                    isDisabled={isPoolExcluded(pool.poolId)}
                    onSelect={() => {
                      setSelectedPoolId(pool.poolId);
                      setShowConnectionCallout(false);
                    }}
                    testId={`pool-row-${pool.name}`}
                  />
                ))}
                {filteredUnknownPools.map((pool) => (
                  <PoolSelectableRow
                    key={pool.poolId}
                    pool={pool}
                    isSelected={false}
                    isDisabled={true}
                    testId={`pool-row-unknown-${pool.poolUrl}`}
                  />
                ))}
              </>
            )}
          </div>
        </div>
      </div>
    </Modal>
  );
};

export default PoolSelectionModal;
