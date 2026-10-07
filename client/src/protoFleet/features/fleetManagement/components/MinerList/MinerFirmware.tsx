import { useId } from "react";
import { createPortal } from "react-dom";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { Activity } from "@/shared/assets/icons";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";
import { useFloatingPosition } from "@/shared/hooks/useFloatingPosition";

type MinerFirmwareProps = {
  miner: MinerStateSnapshot;
  onViewHistory?: () => void;
};

const MinerFirmware = ({ miner, onViewHistory }: MinerFirmwareProps) => {
  const tooltipId = useId();
  const { triggerRef, floatingStyle, isVisible, show, hide } = useFloatingPosition<HTMLButtonElement>({
    placement: "top-start",
    gap: 8,
    minWidth: 192,
  });
  const version = miner.firmwareVersion?.trim() || "Unknown";
  useEscapeDismiss(isVisible && onViewHistory ? hide : undefined);

  if (!onViewHistory) return <span>{version}</span>;

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        className="flex max-w-full cursor-pointer items-center gap-1.5 rounded-sm text-left hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-core-primary-fill"
        aria-label={`View firmware history for ${miner.name || miner.deviceIdentifier}`}
        aria-haspopup="dialog"
        aria-describedby={isVisible ? tooltipId : undefined}
        onMouseEnter={show}
        onMouseLeave={hide}
        onFocus={show}
        onBlur={hide}
        onClick={() => {
          hide();
          onViewHistory();
        }}
      >
        <span className="min-w-0 truncate">{version}</span>
        <span aria-hidden className="shrink-0 text-text-primary-50">
          <Activity width="w-3.5" />
        </span>
      </button>
      {isVisible
        ? createPortal(
            <span
              id={tooltipId}
              role="tooltip"
              className="pointer-events-none fixed z-50 w-48 rounded-lg bg-surface-elevated-base px-3 py-2 text-300 text-text-primary shadow-300"
              style={floatingStyle}
            >
              View firmware history
            </span>,
            document.body,
          )
        : null}
    </>
  );
};

export default MinerFirmware;
