import { type ReactNode } from "react";
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
import {
  type BulkRenamePreferences,
  type BulkRenamePropertyId,
  type BulkRenamePropertyState,
  type BulkRenameSeparatorId,
  bulkRenameSeparators,
  getBulkRenamePropertyDefinition,
} from "./bulkRenameDefinitions";
import { Grip, Slider } from "@/shared/assets/icons";
import Radio from "@/shared/components/Radio";
import Switch from "@/shared/components/Switch";

interface BulkRenamePropertyFormProps {
  preferences: BulkRenamePreferences;
  onDragEnd: (event: DragEndEvent) => void;
  onOpenOptions: (propertyId: BulkRenamePropertyId) => void;
  onToggleEnabled: (propertyId: BulkRenamePropertyId, enabled: boolean) => void;
  onChangeSeparator: (separatorId: BulkRenameSeparatorId) => void;
  propertiesTitle?: string;
  separatorTitle?: string;
  leadingContent?: ReactNode;
}

interface SortablePropertyRowProps {
  property: BulkRenamePropertyState;
  onOpenOptions: (propertyId: BulkRenamePropertyId) => void;
  onToggleEnabled: (propertyId: BulkRenamePropertyId, enabled: boolean) => void;
}

const SortablePropertyRow = ({ property, onOpenOptions, onToggleEnabled }: SortablePropertyRowProps) => {
  const definition = getBulkRenamePropertyDefinition(property.id);
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: property.id });

  return (
    <div
      ref={setNodeRef}
      style={{
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.5 : 1,
      }}
      className="border-b border-border-5"
      data-testid={`bulk-rename-row-${property.id}`}
    >
      <div className="flex h-12 items-center gap-6">
        <button
          type="button"
          className="cursor-grab touch-none text-text-primary hover:text-text-primary active:cursor-grabbing"
          aria-label={`Reorder ${definition.label}`}
          data-testid={`bulk-rename-reorder-${property.id}`}
          {...attributes}
          {...listeners}
        >
          <Grip width="w-4" className="h-4 shrink-0" />
        </button>

        <button
          type="button"
          className="min-w-0 flex-1 text-left text-emphasis-300 text-text-primary"
          aria-label={`Edit ${definition.label} options`}
          data-testid={`bulk-rename-settings-${property.id}`}
          onClick={() => onOpenOptions(property.id)}
        >
          <span className="truncate">{definition.label}</span>
        </button>

        <div className="flex items-center gap-2">
          {property.enabled ? (
            <button
              type="button"
              className="flex h-8 w-8 items-center justify-center rounded-full text-text-primary-70 transition hover:bg-core-primary-5 hover:text-text-primary"
              aria-label={`Open ${definition.label} options`}
              data-testid={`bulk-rename-options-${property.id}`}
              onClick={() => onOpenOptions(property.id)}
            >
              <Slider width="w-4" />
            </button>
          ) : null}
          <Switch checked={property.enabled} setChecked={() => onToggleEnabled(property.id, !property.enabled)} />
        </div>
      </div>
    </div>
  );
};

const BulkRenamePropertyForm = ({
  preferences,
  onDragEnd,
  onOpenOptions,
  onToggleEnabled,
  onChangeSeparator,
  propertiesTitle = "Name properties",
  separatorTitle = "Property separator",
  leadingContent,
}: BulkRenamePropertyFormProps) => {
  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  return (
    <section className="flex flex-col gap-10 pr-6 pb-6 laptop:pr-10 laptop:pb-10 desktop:pr-10 desktop:pb-10">
      {leadingContent}

      <div className="flex flex-col gap-3">
        <h2 className="text-emphasis-300 text-text-primary">{propertiesTitle}</h2>

        <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
          <SortableContext
            items={preferences.properties.map((property) => property.id)}
            strategy={verticalListSortingStrategy}
          >
            <div className="flex flex-col">
              {preferences.properties.map((property) => (
                <SortablePropertyRow
                  key={property.id}
                  property={property}
                  onOpenOptions={onOpenOptions}
                  onToggleEnabled={onToggleEnabled}
                />
              ))}
            </div>
          </SortableContext>
        </DndContext>
      </div>

      <div className="flex flex-col gap-3">
        <h2 className="text-emphasis-300 text-text-primary">{separatorTitle}</h2>
        <div className="flex flex-wrap gap-x-4 gap-y-0 laptop:gap-6 desktop:gap-6">
          {Object.entries(bulkRenameSeparators).map(([separatorId, separator]) => (
            <label key={separatorId} className="flex h-12 items-center">
              <div className="flex w-8 items-center" data-testid={`bulk-rename-separator-${separatorId}`}>
                <Radio
                  name="bulk-rename-separator"
                  value={separatorId}
                  selected={preferences.separator === separatorId}
                  onChange={() => onChangeSeparator(separatorId as BulkRenameSeparatorId)}
                />
              </div>
              <span className="text-300 text-text-primary">{separator.label}</span>
            </label>
          ))}
        </div>
      </div>
    </section>
  );
};

export default BulkRenamePropertyForm;
