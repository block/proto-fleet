import { useCallback, useEffect, useMemo, useState } from "react";

import {
  closestCenter,
  DndContext,
  DragEndEvent,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";

import BackupPoolModalWrapper from "./BackupPoolModalWrapper";
import { Ellipsis, Grip } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import { emptyPoolInfo } from "@/shared/components/MiningPools/constants";
import PoolRow from "@/shared/components/MiningPools/PoolRow";
import { PoolIndex, PoolInfo } from "@/shared/components/MiningPools/types";
import { isValidPool } from "@/shared/components/MiningPools/utility";
import Popover, { popoverSizes } from "@/shared/components/Popover";
import { PopoverProvider, usePopover } from "@/shared/components/Popover";
import Row from "@/shared/components/Row";
import { positions } from "@/shared/constants";
import { pushToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";
import { useClickOutside } from "@/shared/hooks/useClickOutside";
import { debounce, deepClone } from "@/shared/utils/utility";

interface PoolChangeOptions {
  isDelete?: boolean;
}

interface PoolsProps {
  onChangePools: (pools: PoolInfo[], options?: PoolChangeOptions) => void;
  pools: PoolInfo[];
}

const MAX_POOLS = 3;

interface PoolActionsMenuProps {
  onEdit: () => void;
  onDelete: () => void;
  poolIndex: PoolIndex;
  canDelete?: boolean;
}

const PoolActionsMenuInner = ({ onEdit, onDelete, poolIndex, canDelete = true }: PoolActionsMenuProps) => {
  const [isOpen, setIsOpen] = useState(false);
  const { triggerRef } = usePopover();

  const onClickOutside = useCallback(() => {
    setIsOpen(false);
  }, []);

  useClickOutside({
    ref: triggerRef,
    onClickOutside,
    ignoreSelectors: [".popover-content"],
  });

  const handleEdit = useCallback(() => {
    setIsOpen(false);
    onEdit();
  }, [onEdit]);

  const handleDelete = useCallback(() => {
    setIsOpen(false);
    onDelete();
  }, [onDelete]);

  return (
    <div className="relative" ref={triggerRef}>
      <Button
        size={sizes.compact}
        variant={variants.secondary}
        prefixIcon={<Ellipsis width={iconSizes.small} className="text-text-primary-70" />}
        ariaLabel="Pool actions"
        testId={`pool-${poolIndex}-actions-menu-button`}
        onClick={(e) => {
          e.stopPropagation();
          setIsOpen((prev) => !prev);
        }}
      />
      {isOpen ? (
        <Popover
          className="!space-y-0 px-4 pt-2 pb-1"
          position={positions["bottom right"]}
          size={popoverSizes.small}
          offset={8}
          testId={`pool-${poolIndex}-actions-popover`}
        >
          <Row
            className="text-emphasis-300"
            testId={`pool-${poolIndex}-edit-action`}
            onClick={handleEdit}
            compact
            divider={canDelete}
          >
            Edit
          </Row>
          <Row
            className={canDelete ? "text-intent-critical-80" : "cursor-not-allowed text-intent-critical-80 opacity-50"}
            testId={`pool-${poolIndex}-delete-action`}
            onClick={canDelete ? handleDelete : undefined}
            compact
            divider={false}
          >
            Delete
          </Row>
        </Popover>
      ) : null}
    </div>
  );
};

const PoolActionsMenu = (props: PoolActionsMenuProps) => (
  <PopoverProvider>
    <PoolActionsMenuInner {...props} />
  </PopoverProvider>
);

interface SortablePoolRowProps {
  id: string;
  poolIndex: PoolIndex;
  pools: PoolInfo[];
  priorityNumber: number;
  onEdit: () => void;
  onDelete: () => void;
  testId?: string;
  canDelete?: boolean;
}

const SortablePoolRow = ({
  id,
  poolIndex,
  pools,
  priorityNumber,
  onEdit,
  onDelete,
  testId,
  canDelete = true,
}: SortablePoolRowProps) => {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  };

  const gripHandle = (
    <div
      {...attributes}
      {...listeners}
      role="button"
      aria-label="Drag to reorder pool"
      className="cursor-grab touch-none text-text-primary-50 hover:text-text-primary active:cursor-grabbing"
      data-testid={`reorder-handle`}
    >
      <Grip width="w-5" className="h-5 shrink-0" />
    </div>
  );

  const actionsMenu = (
    <PoolActionsMenu onEdit={onEdit} onDelete={onDelete} poolIndex={poolIndex} canDelete={canDelete} />
  );

  return (
    <div ref={setNodeRef} style={style}>
      <PoolRow
        poolIndex={poolIndex}
        pools={pools}
        title={pools[poolIndex]?.name || pools[poolIndex]?.url || "—"}
        subtitleExtra={pools[poolIndex]?.username || undefined}
        priorityNumber={priorityNumber}
        prefixElement={gripHandle}
        suffixElement={actionsMenu}
        onClick={onEdit}
        testId={testId}
      />
    </div>
  );
};

const Pools = ({ onChangePools, pools }: PoolsProps) => {
  const [localPools, setLocalPools] = useState<PoolInfo[]>(deepClone(pools));
  const [isEditing, setIsEditing] = useState(false);
  const [currentPoolIndex, setCurrentPoolIndex] = useState<PoolIndex | null>(null);

  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  const handlePoolsChange = useCallback(
    (newPools: PoolInfo[], options?: PoolChangeOptions) => {
      setLocalPools(newPools);
      onChangePools(newPools, options);
    },
    [onChangePools],
  );

  useEffect(() => {
    if (!isEditing) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- sync local pool draft with upstream pools when not actively editing
      setLocalPools(deepClone(pools));
    }
  }, [isEditing, pools]);

  const debouncedEditDone = useMemo(
    () =>
      debounce(() => {
        setIsEditing(false);
      }),
    [],
  );

  const startEditing = useCallback(() => {
    debouncedEditDone.cancel();
    setIsEditing(true);
  }, [debouncedEditDone]);

  const configuredPools = useMemo(
    () =>
      localPools
        .map((pool, index) => ({ pool, originalIndex: index as PoolIndex }))
        .filter(({ pool }) => isValidPool(pool)),
    [localPools],
  );

  // Use content-based IDs so they remain stable during reordering.
  // If two pools have identical url+username (rare edge case), append a count suffix.
  const configuredPoolIds = useMemo(() => {
    const seen = new Map<string, number>();
    return configuredPools.map(({ pool }) => {
      const baseId = `${pool.url}-${pool.username}`;
      const count = seen.get(baseId) ?? 0;
      seen.set(baseId, count + 1);
      return count > 0 ? `${baseId}-${count}` : baseId;
    });
  }, [configuredPools]);

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;

      if (over && active.id !== over.id) {
        startEditing();

        const oldIndex = configuredPoolIds.indexOf(active.id as string);
        const newIndex = configuredPoolIds.indexOf(over.id as string);

        const reorderedConfigured = arrayMove(configuredPools, oldIndex, newIndex);

        // Rebuild the full pools array with updated priorities
        const newPools = reorderedConfigured.map(({ pool }, index) => ({
          ...pool,
          priority: index,
        }));

        // Fill in empty slots if needed
        while (newPools.length < MAX_POOLS) {
          newPools.push({
            ...emptyPoolInfo,
            priority: newPools.length,
          });
        }

        handlePoolsChange(newPools);
        debouncedEditDone();
      }
    },
    [configuredPools, configuredPoolIds, handlePoolsChange, startEditing, debouncedEditDone],
  );

  const handleDeletePool = useCallback(
    (poolIndex: PoolIndex) => {
      const newPools = localPools
        .filter((_, index) => index !== poolIndex)
        .map((pool, index) => ({ ...pool, priority: index }));

      // Fill in empty slots
      while (newPools.length < MAX_POOLS) {
        newPools.push({
          ...emptyPoolInfo,
          priority: newPools.length,
        });
      }

      handlePoolsChange(newPools, { isDelete: true });
      setCurrentPoolIndex(null);

      pushToast({
        message: "Pool removed",
        status: TOAST_STATUSES.error,
      });
    },
    [localPools, handlePoolsChange],
  );

  const handleAddPool = useCallback(() => {
    // Find the first empty slot
    const emptyIndex = localPools.findIndex((pool) => !isValidPool(pool));
    if (emptyIndex !== -1) {
      startEditing();
      setCurrentPoolIndex(emptyIndex as PoolIndex);
    }
  }, [localPools, startEditing]);

  const handleEditPool = useCallback(
    (poolIndex: PoolIndex) => {
      startEditing();
      setCurrentPoolIndex(poolIndex);
    },
    [startEditing],
  );

  const handleModalDismiss = useCallback(() => {
    debouncedEditDone();
    setCurrentPoolIndex(null);
  }, [debouncedEditDone]);

  const handleModalSave = useCallback(
    (newPools: PoolInfo[]) => {
      debouncedEditDone();
      handlePoolsChange(newPools);
    },
    [debouncedEditDone, handlePoolsChange],
  );

  const isEditMode = currentPoolIndex !== null && isValidPool(localPools[currentPoolIndex]);

  // Empty state
  if (configuredPools.length === 0) {
    return (
      <>
        <div className="flex min-h-[60vh] w-full items-center rounded-xl bg-landing-page p-6 sm:p-20">
          <div className="flex flex-col gap-6">
            <Header
              title="Pools"
              subtitle="Add up to 3 pools for your miner."
              titleSize="text-heading-400"
              subtitleSize="text-400"
              subtitleClassName="mt-1"
            />
            <Button
              text="Add pool"
              variant={variants.primary}
              onClick={handleAddPool}
              testId="add-pool-button"
              className="w-fit"
            />
          </div>
        </div>
        <BackupPoolModalWrapper
          open={currentPoolIndex !== null}
          onChangePools={handleModalSave}
          onDismiss={handleModalDismiss}
          poolIndex={currentPoolIndex ?? 0}
          pools={localPools}
          mode="add"
        />
      </>
    );
  }

  const canDelete = configuredPools.length > 1;

  return (
    <div>
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
        <SortableContext items={configuredPoolIds} strategy={verticalListSortingStrategy}>
          {configuredPools.map(({ originalIndex }, index) => (
            <SortablePoolRow
              key={configuredPoolIds[index]}
              id={configuredPoolIds[index]}
              poolIndex={originalIndex}
              pools={localPools}
              priorityNumber={index + 1}
              onEdit={() => handleEditPool(originalIndex)}
              onDelete={() => handleDeletePool(originalIndex)}
              testId={`pool-${originalIndex}-edit-button`}
              canDelete={canDelete}
            />
          ))}
        </SortableContext>
      </DndContext>

      {configuredPools.length < MAX_POOLS ? (
        <div className="mt-4">
          <Button
            text="Add another pool"
            variant={variants.secondary}
            size={sizes.compact}
            onClick={handleAddPool}
            testId="add-another-pool-button"
          />
        </div>
      ) : null}

      <BackupPoolModalWrapper
        open={currentPoolIndex !== null}
        onChangePools={handleModalSave}
        onDismiss={handleModalDismiss}
        onDelete={isEditMode && canDelete ? () => handleDeletePool(currentPoolIndex!) : undefined}
        poolIndex={currentPoolIndex ?? 0}
        pools={localPools}
        mode={isEditMode ? "edit" : "add"}
      />
    </div>
  );
};

export default Pools;
