import { useMemo, useState } from "react";
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
  arrayMove,
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

export type ColumnPreference<K extends string = string> = { id: K; visible: boolean };
type Props<K extends string> = {
  columns: ColumnPreference<K>[];
  defaultColumns: ColumnPreference<K>[];
  titles: Record<K, string>;
  onDismiss: () => void;
  onSave: (columns: ColumnPreference<K>[]) => void;
};

const SortableColumnRow = <K extends string>({
  column,
  title,
  onToggle,
}: {
  column: ColumnPreference<K>;
  title: string;
  onToggle: (id: K, visible: boolean) => void;
}) => {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: column.id });
  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition, opacity: isDragging ? 0.5 : 1 }}
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
        <Checkbox checked={column.visible} onChange={(event) => onToggle(column.id, event.target.checked)} />
      </label>
    </div>
  );
};

const ManageColumnsModal = <K extends string>({ columns, defaultColumns, titles, onDismiss, onSave }: Props<K>) => {
  const [draft, setDraft] = useState(columns);
  const [previous, setPrevious] = useState(columns);
  if (previous !== columns) {
    setPrevious(columns);
    setDraft(columns);
  }
  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );
  const ids = useMemo(() => draft.map((column) => column.id), [draft]);
  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return;
    setDraft((current) => {
      const from = current.findIndex((column) => column.id === active.id);
      const to = current.findIndex((column) => column.id === over.id);
      if (from < 0 || to < 0) return current;
      return arrayMove(current, from, to);
    });
  };
  return (
    <Modal
      open
      onDismiss={onDismiss}
      title="Manage columns"
      dialogLabel="Manage columns"
      manageFocus
      description="Choose which data to display and rearrange columns to match your workflow."
      buttonSize={sizes.base}
      buttons={[
        {
          text: "Reset to defaults",
          variant: variants.secondary,
          onClick: () => setDraft(defaultColumns),
          testId: "manage-columns-reset-button",
        },
        { text: "Save", variant: variants.primary, onClick: () => onSave(draft), testId: "manage-columns-save-button" },
      ]}
      bodyClassName="text-text-primary"
    >
      <div className="flex flex-col gap-6" data-testid="manage-columns-modal">
        <div className="flex flex-col">
          <div className="border-b border-border-5 py-2 text-emphasis-300 text-text-primary">Column</div>
          <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
            <SortableContext items={ids} strategy={verticalListSortingStrategy}>
              <div className="flex flex-col">
                {draft.map((column) => (
                  <SortableColumnRow
                    key={column.id}
                    column={column}
                    title={titles[column.id]}
                    onToggle={(id, visible) =>
                      setDraft((current) => current.map((item) => (item.id === id ? { ...item, visible } : item)))
                    }
                  />
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
