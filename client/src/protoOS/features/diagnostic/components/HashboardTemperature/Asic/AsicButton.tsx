import { memo } from "react";
import "./AsicButton.css";
import clsx from "clsx";
import { useHashboardLayout } from "@/protoOS/features/diagnostic/hashboardLayout";
import type { AsicData } from "@/protoOS/store";

interface AsicButtonProps {
  asic: AsicData;
  backgroundColor: string;
  isOpen: boolean;
  onSelect: (asic: AsicData, trigger: HTMLDivElement) => void;
  totalAsicCount: number;
  metricValue: string;
  accessibleReading: string;
}

const AsicButton = memo(function AsicButton({
  asic,
  backgroundColor,
  isOpen,
  onSelect,
  totalAsicCount,
  metricValue,
  accessibleReading,
}: AsicButtonProps) {
  const layout = useHashboardLayout();
  const name = layout.labelCell(asic, totalAsicCount);
  return (
    <div
      className={clsx("relative rounded-xl shadow-[0_0_0_3px]", layout.cell.frame, {
        "asic-cell-responsive": layout.cell.metricValueVisibility === "when-wide",
        "shadow-transparent": !isOpen,
        "shadow-intent-info-fill": isOpen,
      })}
    >
      <button
        type="button"
        style={{ backgroundColor }}
        className={clsx(
          "asic-button w-full cursor-pointer truncate rounded-lg border border-border-5 text-center font-mono text-mono-text-50 text-text-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-intent-info-fill",
          layout.cell.fill,
        )}
        aria-label={`ASIC ${name}, ${accessibleReading}`}
        aria-expanded={isOpen}
        aria-controls={isOpen ? "asic-live-readings" : undefined}
        disabled={asic.index === undefined}
        onClick={(event) => onSelect(asic, event.currentTarget.parentElement as HTMLDivElement)}
      >
        <div className={clsx("bg-transparent hover:bg-surface-overlay", layout.cell.fill)}>
          <div className={clsx("flex flex-col items-center gap-1 px-1", layout.cell.content)}>
            <div className="asic-cell-label text-text-primary-50">{name}</div>
            <div className="asic-cell-metric text-mono-text-100 font-mono text-text-primary">
              {metricValue === "—" ? "--" : metricValue}
            </div>
          </div>
        </div>
      </button>
    </div>
  );
});

export default AsicButton;
