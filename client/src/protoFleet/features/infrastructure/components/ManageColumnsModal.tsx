import { useCallback, useMemo, useState } from "react";
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

import { Grip } from "@/shared/assets/icons";
import { sizes, variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Modal from "@/shared/components/Modal";

export interface InfraColumnPreference {
  id: string;
  label: string;
  visible: boolean;
}

interface ManageColumnsModalProps {
  columns: InfraColumnPreference[];
  defaultColumns: InfraColumnPreference[];
  onDismiss: () => void;
  onSave: (columns: InfraColumnPreference[]) => void;
}

const SortableColumnRow = ({
  column,
  onToggle,
}: {
  column: InfraColumnPreference;
  onToggle: (id: string, visible: boolean) => void;
}) => {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: column.id });

  return (
    <div
      ref={setNodeRef}
      style={{
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.5 : 1,
      }}
      className="flex items-center gap-4 border-b border-border-5 py-3 last:border-b-0"
    >
      <button
        type="button"
        className="cursor-grab touch-none text-text-primary hover:text-text-primary active:cursor-grabbing"
        aria-label={`Reorder ${column.label}`}
        {...attributes}
        {...listeners}
      >
        <Grip width="w-4" className="h-4 shrink-0" />
      </button>
      <span className="flex-1 text-emphasis-300 text-text-primary">{column.label}</span>
      <label className="cursor-pointer" aria-label={`Toggle ${column.label} column`}>
        <span className="sr-only">{`Show ${column.label} column`}</span>
        <Checkbox checked={column.visible} onChange={(e) => onToggle(column.id, e.target.checked)} />
      </label>
    </div>
  );
};

const ManageColumnsModal = ({ columns, defaultColumns, onDismiss, onSave }: ManageColumnsModalProps) => {
  const [draft, setDraft] = useState(() => columns);
  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const columnIds = useMemo(() => draft.map((c) => c.id), [draft]);

  const handleDragEnd = useCallback((event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    setDraft((prev) => {
      const oldIdx = prev.findIndex((c) => c.id === active.id);
      const newIdx = prev.findIndex((c) => c.id === over.id);
      if (oldIdx === -1 || newIdx === -1) return prev;
      const next = [...prev];
      const [moved] = next.splice(oldIdx, 1);
      next.splice(newIdx, 0, moved);
      return next;
    });
  }, []);

  const handleToggle = useCallback((id: string, visible: boolean) => {
    setDraft((prev) => prev.map((c) => (c.id === id ? { ...c, visible } : c)));
  }, []);

  const handleReset = useCallback(() => {
    setDraft(defaultColumns.map((c) => ({ ...c })));
  }, [defaultColumns]);

  return (
    <Modal
      open
      onDismiss={onDismiss}
      title="Manage columns"
      description="Choose which data to display and rearrange columns to match your workflow."
      buttonSize={sizes.base}
      buttons={[
        {
          text: "Reset to defaults",
          variant: variants.secondary,
          onClick: handleReset,
          testId: "manage-columns-reset-button",
        },
        {
          text: "Save",
          variant: variants.primary,
          onClick: () => onSave(draft),
          testId: "manage-columns-save-button",
        },
      ]}
      bodyClassName="text-text-primary"
    >
      <div className="flex flex-col gap-6">
        <div className="flex flex-col">
          <div className="border-b border-border-5 py-2 text-emphasis-300 text-text-primary">Column</div>
          <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
            <SortableContext items={columnIds} strategy={verticalListSortingStrategy}>
              <div className="flex flex-col">
                {draft.map((column) => (
                  <SortableColumnRow key={column.id} column={column} onToggle={handleToggle} />
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
