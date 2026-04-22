import { type RefObject, useCallback, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";

import {
  CreatePoolRequestSchema,
  DeletePoolRequestSchema,
  UpdatePoolRequestSchema,
} from "@/protoFleet/api/generated/pools/v1/pools_pb";
import type { Pool } from "@/protoFleet/api/generated/pools/v1/pools_pb";
import usePools from "@/protoFleet/api/usePools";
import Ellipsis from "@/shared/assets/icons/Ellipsis";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import { fleetUsernameHelperText } from "@/shared/components/MiningPools/PoolForm/constants";
import PoolModal from "@/shared/components/MiningPools/PoolModal";
import type { PoolInfo } from "@/shared/components/MiningPools/types";
import { getEmptyPoolsInfo } from "@/shared/components/MiningPools/utility";
import Popover, { PopoverProvider, popoverSizes, usePopover } from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";
import { positions } from "@/shared/constants";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { useClickOutside } from "@/shared/hooks/useClickOutside";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

type ConnectionStatus = "idle" | "testing" | "failed";

interface PoolRowProps {
  pool: Pool;
  onEdit: (pool: Pool) => void;
  onTestConnection: (pool: Pool) => void;
  onDelete: (pool: Pool) => void;
  connectionStatus: ConnectionStatus;
}

// Add zero-width spaces at good break points in URLs
const formatUrlForWrapping = (url: string) => {
  return url
    .replace(/(:\/\/)/g, "$1\u200B") // After ://
    .replace(/(\/)/g, "$1\u200B") // After /
    .replace(/(\.)(?=\w)/g, "$1\u200B") // After . followed by word char
    .replace(/(:)(?=\d)/g, "$1\u200B") // After : followed by digit (port)
    .replace(/(\+)/g, "$1\u200B"); // After +
};

// Add zero-width spaces at good break points in usernames
const formatUsernameForWrapping = (username: string) => {
  return (
    username
      .replace(/(_)/g, "$1\u200B") // After underscore
      .replace(/(\.)(?=\w)/g, "$1\u200B") // After . followed by word char
      .replace(/(-)/g, "$1\u200B") // After hyphen
      // Add break opportunities in long continuous strings every 15 characters
      .replace(/([a-zA-Z0-9]{15})/g, "$1\u200B")
  );
};

// Shared hook for pool row data and menu state
const usePoolRowData = (pool: Pool) => {
  const [showMenu, setShowMenu] = useState(false);
  const { triggerRef } = usePopover();

  // Memoize formatted values to avoid recalculating on every render
  const formattedUrl = useMemo(() => formatUrlForWrapping(pool.url), [pool.url]);
  const formattedUsername = useMemo(
    () => (pool.username ? formatUsernameForWrapping(pool.username) : "—"),
    [pool.username],
  );

  useClickOutside({
    ref: triggerRef,
    onClickOutside: () => setShowMenu(false),
  });

  return { showMenu, setShowMenu, triggerRef, formattedUrl, formattedUsername };
};

// Shared menu component
interface PoolRowMenuProps {
  pool: Pool;
  showMenu: boolean;
  setShowMenu: (show: boolean) => void;
  triggerRef: RefObject<HTMLDivElement | null>;
  onEdit: (pool: Pool) => void;
  onTestConnection: (pool: Pool) => void;
  onDelete: (pool: Pool) => void;
}

const PoolRowMenu = ({
  pool,
  showMenu,
  setShowMenu,
  triggerRef,
  onEdit,
  onTestConnection,
  onDelete,
}: PoolRowMenuProps) => (
  <div className="relative" ref={triggerRef}>
    <Button
      variant="ghost"
      size="compact"
      onClick={(e) => {
        e.stopPropagation();
        setShowMenu(!showMenu);
      }}
      ariaLabel="Options menu"
      ariaHasPopup="menu"
      ariaExpanded={showMenu}
      prefixIcon={<Ellipsis />}
    />
    {showMenu && (
      <Popover
        position={positions["top left"]}
        size={popoverSizes.small}
        className="rounded-2xl! px-4 pt-2 pb-1"
        xOffset={-30}
        yOffset={40}
      >
        {/* Stop propagation so menu item clicks don't bubble to the row onClick */}
        <div className="flex flex-col" onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
          <Row
            onClick={() => {
              onEdit(pool);
              setShowMenu(false);
            }}
            compact
          >
            Edit pool
          </Row>
          <Row
            onClick={() => {
              onTestConnection(pool);
              setShowMenu(false);
            }}
            compact
          >
            Test connection
          </Row>
          <Row
            onClick={() => {
              onDelete(pool);
              setShowMenu(false);
            }}
            divider={false}
            compact
            className="text-intent-critical-80"
          >
            Delete pool
          </Row>
        </div>
      </Popover>
    )}
  </div>
);

const PoolRowDesktop = ({ pool, onEdit, onTestConnection, onDelete, connectionStatus }: PoolRowProps) => {
  const { showMenu, setShowMenu, triggerRef, formattedUrl, formattedUsername } = usePoolRowData(pool);

  return (
    <div
      className="grid cursor-pointer grid-cols-3 gap-1 rounded-lg text-300 text-text-primary transition-colors duration-200 hover:bg-core-primary-5"
      data-testid="pool-row"
      onClick={() => onEdit(pool)}
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.target !== e.currentTarget) return;
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onEdit(pool);
        }
      }}
    >
      <div className="flex items-center py-3" data-testid="pool-name">
        {pool.poolName || "—"}
      </div>
      <div className="flex flex-col justify-center py-3" data-testid="pool-url">
        <span className="break-all">{formattedUrl}</span>
        {connectionStatus === "failed" && <span className="text-200 text-text-critical">Connection failed</span>}
      </div>
      <div className="flex items-center justify-between gap-4 py-3" data-testid="pool-username">
        <span className="break-all">{formattedUsername}</span>
        <PoolRowMenu
          pool={pool}
          showMenu={showMenu}
          setShowMenu={setShowMenu}
          triggerRef={triggerRef}
          onEdit={onEdit}
          onTestConnection={onTestConnection}
          onDelete={onDelete}
        />
      </div>
    </div>
  );
};

const PoolRowMobile = ({ pool, onEdit, onTestConnection, onDelete, connectionStatus }: PoolRowProps) => {
  const { showMenu, setShowMenu, triggerRef, formattedUrl, formattedUsername } = usePoolRowData(pool);

  return (
    <div
      className="grid cursor-pointer grid-cols-2 items-start rounded-lg py-4 transition-colors duration-200 hover:bg-core-primary-5"
      data-testid="pool-row"
      onClick={() => onEdit(pool)}
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.target !== e.currentTarget) return;
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onEdit(pool);
        }
      }}
    >
      {/* Left column: Pool name and URL */}
      <div className="flex flex-col">
        {pool.poolName ? (
          <>
            <div className="text-300 text-text-primary" data-testid="pool-name">
              {pool.poolName}
            </div>
            <div className="text-200 break-all text-text-primary-70" data-testid="pool-url">
              {formattedUrl}
            </div>
          </>
        ) : (
          <div className="text-300 break-all text-text-primary" data-testid="pool-url">
            {formattedUrl}
          </div>
        )}
        {connectionStatus === "failed" && <span className="text-200 text-text-critical">Connection failed</span>}
      </div>

      {/* Right column: Username and ellipsis */}
      <div className="flex items-center justify-between gap-4 self-center" data-testid="pool-username">
        <div className="text-300 break-all text-text-primary">{formattedUsername}</div>
        <PoolRowMenu
          pool={pool}
          showMenu={showMenu}
          setShowMenu={setShowMenu}
          triggerRef={triggerRef}
          onEdit={onEdit}
          onTestConnection={onTestConnection}
          onDelete={onDelete}
        />
      </div>
    </div>
  );
};

const PoolRowInner = ({ pool, onEdit, onTestConnection, onDelete, connectionStatus }: PoolRowProps) => {
  const { isPhone, isTablet } = useWindowDimensions();
  const isMobile = isPhone || isTablet;

  return (
    <>
      <PopoverProvider>
        {isMobile ? (
          <PoolRowMobile
            pool={pool}
            onEdit={onEdit}
            onTestConnection={onTestConnection}
            onDelete={onDelete}
            connectionStatus={connectionStatus}
          />
        ) : (
          <PoolRowDesktop
            pool={pool}
            onEdit={onEdit}
            onTestConnection={onTestConnection}
            onDelete={onDelete}
            connectionStatus={connectionStatus}
          />
        )}
      </PopoverProvider>
      <div className="border-b border-core-primary-10" />
    </>
  );
};

const MiningPools = () => {
  const { pools, createPool, updatePool, deletePool, validatePool, validatePoolPending, isLoading } = usePools();
  const { isPhone, isTablet } = useWindowDimensions();
  const [showAddPoolModal, setShowAddPoolModal] = useState(false);
  const [showEditPoolModal, setShowEditPoolModal] = useState(false);
  const [poolsInfo, setPoolsInfo] = useState<PoolInfo[]>(getEmptyPoolsInfo());
  const [editingPool, setEditingPool] = useState<Pool | null>(null);
  const [connectionStatuses, setConnectionStatuses] = useState<Record<string, ConnectionStatus>>({});

  const handleEditPool = useCallback((pool: Pool) => {
    setEditingPool(pool);
    // Convert Pool to PoolInfo format for the modal
    setPoolsInfo([
      {
        name: pool.poolName || "",
        url: pool.url,
        username: pool.username,
        password: "",
        priority: 0,
      },
    ]);
    setShowEditPoolModal(true);
  }, []);

  const handleTestConnection = useCallback(
    (pool: Pool) => {
      const poolId = pool.poolId.toString();

      setConnectionStatuses((prev) => ({ ...prev, [poolId]: "testing" }));

      validatePool({
        poolInfo: {
          url: pool.url,
          username: pool.username,
          password: "",
        },
        onSuccess: () => {
          setConnectionStatuses((prev) => ({ ...prev, [poolId]: "idle" }));
          pushToast({
            message: "Pool connection successful",
            status: STATUSES.success,
          });
        },
        onError: () => {
          setConnectionStatuses((prev) => ({ ...prev, [poolId]: "failed" }));
        },
      });
    },
    [validatePool],
  );

  const handleSavePool = useCallback(
    async (pool: PoolInfo, isPasswordSet: boolean) => {
      const passwordToSend = isPasswordSet ? pool.password : "";

      const createPoolRequest = create(CreatePoolRequestSchema, {
        poolConfig: {
          poolName: pool.name || "",
          url: pool.url,
          username: pool.username,
          password: passwordToSend,
        },
      });

      await createPool({
        createPoolRequest,
        onSuccess: () => {
          pushToast({
            message: "Pool added",
            status: STATUSES.success,
          });
          setShowAddPoolModal(false);
          // Reset pools info for next time
          setPoolsInfo(getEmptyPoolsInfo());
        },
        onError: (error) => {
          throw error;
        },
      });
    },
    [createPool],
  );

  const handleUpdatePool = useCallback(
    async (pool: PoolInfo, isPasswordSet: boolean) => {
      if (!editingPool) return;

      const updatePoolRequest = create(UpdatePoolRequestSchema, {
        poolId: editingPool.poolId,
        poolName: pool.name || "",
        url: pool.url,
        username: pool.username,
        password: isPasswordSet ? pool.password : undefined,
      });

      await updatePool({
        updatePoolRequest,
        onSuccess: () => {
          pushToast({
            message: "Pool updated",
            status: STATUSES.success,
          });
          setShowEditPoolModal(false);
          setEditingPool(null);
          setPoolsInfo(getEmptyPoolsInfo());
        },
        onError: (error) => {
          throw error;
        },
      });
    },
    [editingPool, updatePool],
  );

  const handleDeletePool = useCallback(
    (pool: Pool) => {
      const deletePoolRequest = create(DeletePoolRequestSchema, {
        poolId: pool.poolId,
      });

      deletePool({
        deletePoolRequest,
        onSuccess: () => {
          pushToast({
            message: "Pool deleted",
            status: STATUSES.success,
          });
          // Close edit modal if the deleted pool was being edited
          if (editingPool?.poolId === pool.poolId) {
            setShowEditPoolModal(false);
            setEditingPool(null);
            setPoolsInfo(getEmptyPoolsInfo());
          }
        },
        onError: (error) => {
          pushToast({
            message: error || "Failed to delete pool",
            status: STATUSES.error,
          });
        },
      });
    },
    [deletePool, editingPool],
  );

  // Loading state - show spinner while fetching
  if (isLoading) {
    return (
      <div className="flex justify-center py-20">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  return (
    <>
      <div className="flex flex-col gap-6">
        <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
          <Header
            title="Pools"
            description="Add and manage the pools for your fleet."
            titleSize="text-heading-300"
          />
          <Button
            variant={variants.primary}
            size={sizes.compact}
            onClick={() => setShowAddPoolModal(true)}
            className="shrink-0 phone:w-full"
          >
            Add pool
          </Button>
        </div>

        <div className="flex flex-col">
          {/* Conditional header based on screen size */}
          {isPhone || isTablet ? (
            /* Mobile & Tablet: Card header */
            <div className="grid grid-cols-2 border-b border-core-primary-10 py-3 text-emphasis-300 text-text-primary-50">
              <div>Name</div>
              <div className="flex items-center justify-between">
                <div>Username</div>
              </div>
            </div>
          ) : (
            /* Desktop: Table header */
            <div className="grid grid-cols-3 gap-1 text-emphasis-300 text-text-primary-50">
              <Row>Name</Row>
              <Row>URL</Row>
              <Row>Username</Row>
            </div>
          )}

          {/* Table body */}
          <div>
            {pools.length === 0 ? (
              <div className="py-10 text-center text-text-primary-50">
                No pools yet. Add a pool to start assigning your miners.
              </div>
            ) : (
              pools.map((pool) => (
                <PoolRowInner
                  key={pool.poolId.toString()}
                  pool={pool}
                  onEdit={handleEditPool}
                  onTestConnection={handleTestConnection}
                  onDelete={handleDeletePool}
                  connectionStatus={connectionStatuses[pool.poolId.toString()] || "idle"}
                />
              ))
            )}
          </div>
        </div>
      </div>

      <PoolModal
        open={showAddPoolModal}
        onChangePools={setPoolsInfo}
        onDismiss={() => setShowAddPoolModal(false)}
        poolIndex={0}
        pools={poolsInfo}
        isTestingConnection={validatePoolPending}
        testConnection={validatePool}
        onSave={handleSavePool}
        usernameHelperText={fleetUsernameHelperText}
        disallowUsernameSeparator
      />

      <PoolModal
        open={showEditPoolModal}
        onChangePools={setPoolsInfo}
        onDismiss={() => {
          setShowEditPoolModal(false);
          setEditingPool(null);
          setPoolsInfo(getEmptyPoolsInfo());
        }}
        poolIndex={0}
        pools={poolsInfo}
        isTestingConnection={validatePoolPending}
        testConnection={(args) => {
          validatePool({
            ...args,
            onSuccess: () => {
              if (editingPool) {
                setConnectionStatuses((prev) => ({ ...prev, [editingPool.poolId.toString()]: "idle" }));
              }
              args.onSuccess?.();
            },
          });
        }}
        onSave={handleUpdatePool}
        mode="edit"
        onDelete={editingPool ? () => handleDeletePool(editingPool) : undefined}
        usernameHelperText={fleetUsernameHelperText}
        disallowUsernameSeparator
      />
    </>
  );
};

export default MiningPools;
