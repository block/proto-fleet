import clsx from "clsx";

import { type BuildingAssignmentMode } from "./types";
import { variants } from "@/shared/components/Button";
import Button, { sizes } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";

interface AssignedRackRow {
  rackId: bigint;
  label: string;
  // Position string for the inline secondary line; undefined when not placed.
  positionLabel?: string;
}

interface BuildingRacksPaneProps {
  aislesText: string;
  racksPerAisleText: string;
  aislesError: string | null;
  racksPerAisleError: string | null;
  onAislesChange: (value: string) => void;
  onRacksPerAisleChange: (value: string) => void;
  assignmentMode: BuildingAssignmentMode;
  onModeChange: (mode: BuildingAssignmentMode) => void;
  assignedRacks: AssignedRackRow[];
  selectedRackId: bigint | null;
  onSelectRack: (rackId: bigint | null) => void;
  onRemoveRack: (rackId: bigint) => void;
  onOpenSearchRacks: () => void;
  saving?: boolean;
}

const ModeButton = ({
  active,
  onClick,
  children,
  testId,
}: {
  active: boolean;
  onClick: () => void;
  children: string;
  testId: string;
}) => (
  <button
    type="button"
    onClick={onClick}
    className={clsx(
      "flex-1 rounded-lg border px-3 py-2 text-emphasis-300",
      active ? "border-border-100 bg-surface-base text-text-primary" : "border-border-5 text-text-primary-70",
    )}
    data-testid={testId}
  >
    {children}
  </button>
);

const BuildingRacksPane = ({
  aislesText,
  racksPerAisleText,
  aislesError,
  racksPerAisleError,
  onAislesChange,
  onRacksPerAisleChange,
  assignmentMode,
  onModeChange,
  assignedRacks,
  selectedRackId,
  onSelectRack,
  onRemoveRack,
  onOpenSearchRacks,
  saving = false,
}: BuildingRacksPaneProps) => {
  return (
    <div className="flex flex-col gap-6 pr-6 pb-6 laptop:pr-10 laptop:pb-10">
      <section className="flex flex-col gap-3">
        <Header title="Layout" titleSize="text-heading-100" />
        <div className="flex gap-3">
          <div className="flex-1">
            <Input
              id="manage-building-aisles"
              label="Aisles"
              type="number"
              initValue={aislesText}
              onChange={onAislesChange}
              error={aislesError ?? false}
              disabled={saving}
              testId="manage-building-aisles-input"
            />
          </div>
          <div className="flex-1">
            <Input
              id="manage-building-racks-per-aisle"
              label="Racks per aisle"
              type="number"
              initValue={racksPerAisleText}
              onChange={onRacksPerAisleChange}
              error={racksPerAisleError ?? false}
              disabled={saving}
              testId="manage-building-racks-per-aisle-input"
            />
          </div>
        </div>
      </section>

      <section className="flex flex-col gap-3">
        <div className="flex items-center justify-between">
          <Header title="Racks" titleSize="text-heading-100" />
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="Assign racks"
            onClick={onOpenSearchRacks}
            disabled={saving}
            testId="manage-building-assign-racks"
          />
        </div>
        <div className="flex gap-2">
          <ModeButton
            active={assignmentMode === "byName"}
            onClick={() => onModeChange("byName")}
            testId="manage-building-mode-byname"
          >
            By name
          </ModeButton>
          <ModeButton
            active={assignmentMode === "manual"}
            onClick={() => onModeChange("manual")}
            testId="manage-building-mode-manual"
          >
            Manual
          </ModeButton>
        </div>
        {assignedRacks.length === 0 ? (
          <div className="rounded-xl border border-dashed border-border-5 p-4 text-center text-300 text-text-primary-50">
            No racks assigned yet. Click "Assign racks" to add one.
          </div>
        ) : (
          <ul className="flex flex-col" data-testid="manage-building-assigned-racks">
            {assignedRacks.map((row) => {
              const isSelected = selectedRackId === row.rackId;
              return (
                <li
                  key={row.rackId.toString()}
                  className={clsx(
                    "flex items-center justify-between gap-2 border-b border-border-5 py-2",
                    isSelected && "bg-surface-base-hover",
                  )}
                  data-testid={`manage-building-assigned-rack-${row.rackId.toString()}`}
                >
                  <button
                    type="button"
                    onClick={() =>
                      assignmentMode === "manual" ? onSelectRack(isSelected ? null : row.rackId) : undefined
                    }
                    disabled={assignmentMode !== "manual" || saving}
                    className={clsx(
                      "flex flex-1 flex-col items-start gap-0.5 text-left",
                      assignmentMode === "manual" && !saving && "cursor-pointer",
                    )}
                  >
                    <span className="truncate text-emphasis-300">{row.label || "(unnamed rack)"}</span>
                    {row.positionLabel ? (
                      <span className="text-300 text-text-primary-50">{row.positionLabel}</span>
                    ) : null}
                  </button>
                  <button
                    type="button"
                    onClick={() => onRemoveRack(row.rackId)}
                    disabled={saving}
                    className="px-2 py-1 text-300 text-text-primary-50 hover:text-intent-critical-fill"
                    data-testid={`manage-building-remove-rack-${row.rackId.toString()}`}
                  >
                    Remove
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </div>
  );
};

export default BuildingRacksPane;
export type { AssignedRackRow, BuildingRacksPaneProps };
