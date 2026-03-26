import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";

import { create } from "@bufbuild/protobuf";
import type { AssignmentMode } from "./types";
import { DeviceIdentifierListSchema } from "@/protoFleet/api/generated/common/v1/device_selector_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { BlinkLEDRequestSchema, DeviceSelectorSchema } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";
import { computeSlotNumber, type NumberingOrigin } from "@/protoFleet/features/rackManagement/utils/slotNumbering";

import { ArrowRight, Checkmark, DismissTiny, Ellipsis } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import { pushToast, STATUSES } from "@/shared/features/toaster";

interface MinersPaneProps {
  rackMiners: string[];
  miners: Record<string, MinerStateSnapshot>;
  slotAssignments: Record<string, string>;
  assignmentMode: AssignmentMode;
  selectedMinerId: string | null;
  rows: number;
  cols: number;
  numberingOrigin: NumberingOrigin;
  onModeChange: (mode: AssignmentMode) => void;
  onSelectMiner: (deviceId: string | null) => void;
  onRemoveMiner: (deviceId: string) => void;
  onUnassignMiner: (deviceId: string) => void;
  onClearAssignments: () => void;
  onOpenManageMiners: () => void;
}

const modeSegments = [
  { key: "manual", title: "Assign manually" },
  { key: "byName", title: "Assign by name" },
  { key: "byNetwork", title: "Assign by network" },
];

function MinerRow({
  deviceId,
  miner,
  assignedSlotNumber,
  assignmentMode,
  isSelected,
  onSelect,
  onRemove,
  onUnassign,
  onBlinkLED,
}: {
  deviceId: string;
  miner: MinerStateSnapshot | undefined;
  assignedSlotNumber: number | null;
  assignmentMode: AssignmentMode;
  isSelected: boolean;
  onSelect: (deviceId: string | null) => void;
  onRemove: (deviceId: string) => void;
  onUnassign: (deviceId: string) => void;
  onBlinkLED: (deviceId: string) => void;
}) {
  const name = miner?.name;
  const ipAddress = miner?.ipAddress;
  const macAddress = miner?.macAddress;
  const model = miner?.model;
  const [showMenu, setShowMenu] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  const isAssigned = assignedSlotNumber !== null;
  const isManual = assignmentMode === "manual";
  const isClickable = isManual;

  const handleClick = () => {
    if (!isClickable) return;
    if (isSelected) {
      onSelect(null);
    } else {
      onSelect(deviceId);
    }
  };

  const handleBlinkLED = useCallback(() => {
    setShowMenu(false);
    onBlinkLED(deviceId);
  }, [deviceId, onBlinkLED]);

  const handleRemove = useCallback(() => {
    setShowMenu(false);
    onRemove(deviceId);
  }, [deviceId, onRemove]);

  // Close menu on Escape
  useEffect(() => {
    if (!showMenu) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") setShowMenu(false);
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [showMenu]);

  const subtitleParts = [ipAddress, macAddress, model].filter(Boolean);
  const hasIcon = isSelected || isAssigned;

  return (
    <div
      className={clsx(
        "flex items-center px-3 py-3 transition-colors",
        isSelected && "bg-surface-5",
        isClickable && !isSelected && "cursor-pointer hover:bg-surface-5",
        !isClickable && "cursor-default",
      )}
      onClick={handleClick}
    >
      <div
        className="shrink-0 overflow-hidden transition-all duration-300"
        style={{ width: hasIcon ? 32 : 0, marginRight: hasIcon ? 8 : 0 }}
      >
        <div
          className={clsx(
            "flex h-6 w-6 items-center justify-center rounded-full",
            isSelected && "bg-core-primary-fill",
            !isSelected && isAssigned && "bg-intent-success-fill",
          )}
        >
          {isSelected && <ArrowRight width="w-3" className="text-white" />}
          {!isSelected && isAssigned && <Checkmark width="w-4" className="text-white" />}
        </div>
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-300 text-text-primary">{name || deviceId}</div>
        {subtitleParts.length > 0 && (
          <div className="truncate text-300 text-text-primary-50">{subtitleParts.join(", ")}</div>
        )}
      </div>

      {isSelected && <span className="shrink-0 text-200 text-text-primary">select rack position</span>}

      {!isSelected && isAssigned && (
        <span className="shrink-0 text-300 font-medium text-text-primary tabular-nums">
          Position {String(assignedSlotNumber).padStart(2, "0")}
        </span>
      )}

      <div className="relative shrink-0" ref={menuRef}>
        {isAssigned ? (
          <button
            type="button"
            className="flex h-8 w-8 items-center justify-center rounded-lg text-text-primary-70 hover:cursor-pointer"
            onClick={(e) => {
              e.stopPropagation();
              onUnassign(deviceId);
            }}
          >
            <DismissTiny />
          </button>
        ) : (
          <>
            <button
              type="button"
              className="flex h-8 w-8 items-center justify-center rounded-lg text-text-primary-70 hover:cursor-pointer"
              onClick={(e) => {
                e.stopPropagation();
                setShowMenu((prev) => !prev);
              }}
            >
              <Ellipsis width="w-4" />
            </button>
            {showMenu && (
              <>
                <div
                  className="fixed inset-0 z-20"
                  onClick={(e) => {
                    e.stopPropagation();
                    setShowMenu(false);
                  }}
                />
                <div className="absolute top-full right-0 z-30 mt-1 w-44 rounded-xl border border-border-5 bg-surface-elevated-base py-1 shadow-300">
                  <button
                    type="button"
                    className="w-full px-4 py-2 text-left text-300 text-text-primary hover:bg-surface-2"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleRemove();
                    }}
                  >
                    Remove miner
                  </button>
                  <button
                    type="button"
                    className="w-full px-4 py-2 text-left text-300 text-text-primary hover:bg-surface-5"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleBlinkLED();
                    }}
                  >
                    Blink LEDs
                  </button>
                </div>
              </>
            )}
          </>
        )}
      </div>
    </div>
  );
}

export default function MinersPane({
  rackMiners,
  miners,
  slotAssignments,
  assignmentMode,
  selectedMinerId,
  rows,
  cols,
  numberingOrigin,
  onModeChange,
  onSelectMiner,
  onRemoveMiner,
  onUnassignMiner,
  onClearAssignments,
  onOpenManageMiners,
}: MinersPaneProps) {
  const { blinkLED } = useMinerCommand();

  const handleBlinkLED = useCallback(
    (deviceId: string) => {
      const request = create(BlinkLEDRequestSchema, {
        deviceSelector: create(DeviceSelectorSchema, {
          selectionType: {
            case: "includeDevices",
            value: create(DeviceIdentifierListSchema, {
              deviceIdentifiers: [deviceId],
            }),
          },
        }),
      });
      blinkLED({
        blinkLEDRequest: request,
        onSuccess: () => {
          pushToast({ message: "Blink LED command sent", status: STATUSES.success });
        },
        onError: (error) => {
          pushToast({ message: error, status: STATUSES.error });
        },
      });
    },
    [blinkLED],
  );

  // Pre-compute deviceId -> slotNumber reverse map for O(1) lookups per MinerRow
  const slotNumberByDevice = useMemo(() => {
    const map: Record<string, number> = {};
    for (const [key, deviceId] of Object.entries(slotAssignments)) {
      const [row, col] = key.split("-").map(Number);
      map[deviceId] = computeSlotNumber(row, col, rows, cols, numberingOrigin);
    }
    return map;
  }, [slotAssignments, rows, cols, numberingOrigin]);

  if (rackMiners.length === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-4">
        <p className="text-300 text-text-primary-50">No miners added to this rack yet.</p>
        <Button variant={variants.primary} size={buttonSizes.compact} onClick={onOpenManageMiners}>
          Add miners
        </Button>
      </div>
    );
  }

  // Sort miners alphabetically by deviceId (stable sort for consistent ordering)
  const sortedMiners = [...rackMiners].sort((a, b) => a.localeCompare(b));

  return (
    <div className="flex flex-col gap-4 p-4">
      <div className="flex flex-col gap-3">
        <h3 className="text-emphasis-300 text-text-primary">Assign miners</h3>
        <div className="flex items-center gap-2">
          <div className="flex items-center gap-1">
            {modeSegments.map((seg) => (
              <Button
                key={seg.key}
                variant={assignmentMode === seg.key ? variants.primary : variants.secondary}
                size={buttonSizes.compact}
                onClick={() => onModeChange(seg.key as AssignmentMode)}
              >
                {seg.title}
              </Button>
            ))}
          </div>
          <div className="ml-auto">
            <button
              type="button"
              className="text-300 text-core-primary-fill hover:underline"
              onClick={onClearAssignments}
            >
              Clear
            </button>
          </div>
        </div>
      </div>

      <div className="flex flex-col divide-y divide-border-5">
        {sortedMiners.map((deviceId) => (
          <MinerRow
            key={deviceId}
            deviceId={deviceId}
            miner={miners[deviceId]}
            assignedSlotNumber={slotNumberByDevice[deviceId] ?? null}
            assignmentMode={assignmentMode}
            isSelected={selectedMinerId === deviceId}
            onSelect={onSelectMiner}
            onRemove={onRemoveMiner}
            onUnassign={onUnassignMiner}
            onBlinkLED={handleBlinkLED}
          />
        ))}
      </div>
    </div>
  );
}
