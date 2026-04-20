import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import FleetPoolActionsMenu from "./FleetPoolActionsMenu";
import { MiningPool } from "./types";
import { Grip } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Row from "@/shared/components/Row";

interface FleetPoolRowProps {
  pool: MiningPool;
  priorityNumber: number;
  onUpdate: () => void;
  onTestConnection: () => void;
  onRemove: () => void;
  testId?: string;
}

const FleetPoolRow = ({ pool, priorityNumber, onUpdate, onTestConnection, onRemove, testId }: FleetPoolRowProps) => {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: pool.poolId });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  };

  const displayTitle = pool.name || pool.poolUrl || "—";

  return (
    <div ref={setNodeRef} style={style} data-testid={testId}>
      <Row className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          {/* Priority number */}
          <div className="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full bg-surface-5 text-xs font-medium text-text-primary">
            {priorityNumber}
          </div>

          {/* Drag handle */}
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

          {/* Pool info */}
          <div className="flex min-w-0 flex-col">
            <div className="truncate text-text-primary" data-testid="pool-name">
              {displayTitle}
            </div>
            <div className="truncate text-200 text-text-primary-70" data-testid="pool-url">
              {pool.poolUrl}
            </div>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="Update"
            onClick={onUpdate}
            testId={`${testId}-update-button`}
          />
          <FleetPoolActionsMenu onTestConnection={onTestConnection} onRemove={onRemove} poolId={pool.poolId} />
        </div>
      </Row>
    </div>
  );
};

export default FleetPoolRow;
