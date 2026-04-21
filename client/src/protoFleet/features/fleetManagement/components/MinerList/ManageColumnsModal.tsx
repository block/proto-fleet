import { useCallback, useEffect, useMemo, useState } from "react";
import {
  closestCenter,
  DndContext,
  type DragEndEvent,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { minerColTitles } from "./constants";
import {
  type ConfigurableMinerColumn,
  createDefaultMinerTableColumnPreferences,
  type MinerTableColumnPreference,
  type MinerTableColumnPreferences,
  reorderMinerTableColumns,
  updateMinerTableColumnVisibility,
} from "./minerTableColumnPreferences";
import { Dismiss, Grip } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Modal from "@/shared/components/Modal";

type ManageColumnsModalProps = {
  preferences: MinerTableColumnPreferences;
  onDismiss: () => void;
  onSave: (preferences: MinerTableColumnPreferences) => void;
};

type SortableColumnRowProps = {
  column: MinerTableColumnPreference;
  onToggleVisible: (columnId: ConfigurableMinerColumn, visible: boolean) => void;
};

const SortableColumnRow = ({ column, onToggleVisible }: SortableColumnRowProps) => {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: column.id });
  const title = minerColTitles[column.id];

  return (
    <div
      ref={setNodeRef}
      style={{
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.5 : 1,
      }}
      className="flex items-center gap-4 border-b border-border-5 py-3 last:border-b-0"
      data-testid={`manage-columns-row-${column.id}`}
    >
      <button
        type="button"
        className="cursor-grab touch-none text-text-primary hover:text-text-primary active:cursor-grabbing"
        aria-label={`Reorder ${title}`}
        data-testid={`manage-columns-reorder-${column.id}`}
        {...attributes}
        {...listeners}
      >
        <Grip width="w-4" className="h-4 shrink-0" />
      </button>

      <span className="flex-1 text-emphasis-300 text-text-primary">{title}</span>

      <label className="cursor-pointer" aria-label={`Toggle ${title} column`}>
        <span className="sr-only">{`Show ${title} column`}</span>
        <Checkbox checked={column.visible} onChange={(event) => onToggleVisible(column.id, event.target.checked)} />
      </label>
    </div>
  );
};

const ManageColumnsModal = ({ preferences, onDismiss, onSave }: ManageColumnsModalProps) => {
  const [draftPreferences, setDraftPreferences] = useState(() => preferences);
  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  useEffect(() => {
    setDraftPreferences(preferences);
  }, [preferences]);

  const handleDragEnd = useCallback((event: DragEndEvent) => {
    const { active, over } = event;

    if (!over || active.id === over.id) {
      return;
    }

    setDraftPreferences((current) =>
      reorderMinerTableColumns(current, active.id as ConfigurableMinerColumn, over.id as ConfigurableMinerColumn),
    );
  }, []);

  const handleToggleVisible = useCallback((columnId: ConfigurableMinerColumn, visible: boolean) => {
    setDraftPreferences((current) => updateMinerTableColumnVisibility(current, columnId, visible));
  }, []);

  const handleResetToDefaults = useCallback(() => {
    setDraftPreferences(createDefaultMinerTableColumnPreferences());
  }, []);

  const handleSave = useCallback(() => {
    onSave(draftPreferences);
  }, [draftPreferences, onSave]);

  const columnIds = useMemo(() => draftPreferences.columns.map((column) => column.id), [draftPreferences.columns]);

  return (
    <Modal open onDismiss={onDismiss} showHeader={false} bodyClassName="text-text-primary">
      <div className="flex flex-col gap-6" data-testid="manage-columns-modal">
        <div className="flex items-center gap-4">
          <Button
            ariaLabel="Close dialog"
            variant={variants.secondary}
            prefixIcon={<Dismiss width="w-5" />}
            onClick={onDismiss}
          />

          <div className="ml-auto flex items-center gap-3">
            <Button
              text="Reset to defaults"
              variant={variants.secondary}
              size={sizes.base}
              onClick={handleResetToDefaults}
            />
            <Button text="Save" variant={variants.primary} size={sizes.base} onClick={handleSave} />
          </div>
        </div>

        <div className="flex flex-col gap-1">
          <h2 className="text-heading-300 text-text-primary">Manage columns</h2>
          <p className="text-300 text-text-primary-70">
            Choose which data to display and rearrange columns to match your workflow.
          </p>
        </div>

        <div className="flex flex-col">
          <div className="border-b border-border-5 py-2 text-emphasis-300 text-text-primary">Column</div>

          <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
            <SortableContext items={columnIds} strategy={verticalListSortingStrategy}>
              <div className="flex flex-col">
                {draftPreferences.columns.map((column) => (
                  <SortableColumnRow key={column.id} column={column} onToggleVisible={handleToggleVisible} />
                ))}
              </div>
            </SortableContext>
          </DndContext>
        </div>
      </div>
    </Modal>
  );
};

export default ManageColumnsModal;
