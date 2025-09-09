import { useMemo } from "react";
import clsx from "clsx";
import { Circle } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import { type HashboardLocationStore } from "@/shared/features/kpis/components/KpiLineChart/KpiTooltip";
import { type TimeSeriesWithSerial } from "@/shared/features/kpis/components/KpiLineChart/types";
import { getHashboardColor } from "@/shared/features/kpis/components/KpiLineChart/utility";
import useCssVariable from "@/shared/hooks/useCssVariable";

// TODO: remove this when we update to new API

type HashboardSelectorItemProps = {
  slot: number | null;
  onClick: () => void;
  variant: (typeof variants)[keyof typeof variants];
};

const HashboardSelectorItem = ({
  slot,
  onClick,
  variant,
}: HashboardSelectorItemProps) => {
  const colorVariable = useMemo(() => {
    if (!slot) return "";
    return getHashboardColor(slot);
  }, [slot]);

  const color = useCssVariable(colorVariable);

  if (slot === null) return null;

  return (
    <Button
      key={"hashboard-selector-" + slot}
      size={sizes.compact}
      variant={variant}
      prefixIcon={
        <Circle
          className={clsx("mr-1")}
          width={"w-2"}
          style={{ background: color }}
        />
      }
      text={slot.toString()}
      onClick={onClick}
    />
  );
};

type HashboardSelectorProps = {
  series: TimeSeriesWithSerial[];
  hashboardLocationStore: HashboardLocationStore;
  setActiveHashboards: (serials: string[]) => void;
  activeHashboards: string[];
  showAggregate: boolean;
  setShowAggregate: (show: boolean) => void;
  className?: string;
};

const HashboardSelector = ({
  series,
  hashboardLocationStore,
  setActiveHashboards,
  activeHashboards,
  showAggregate,
  setShowAggregate,
  className = "",
}: HashboardSelectorProps) => {
  const { getSlotByHbSn } = hashboardLocationStore;

  const handleAllHashboardsClick = () => {
    if (activeHashboards.length === series.length) {
      setActiveHashboards([]);
    } else {
      setActiveHashboards(series.map((s) => s.serial));
    }
  };

  const handleHashboardClick = (serial: string) => {
    if (activeHashboards.includes(serial)) {
      setActiveHashboards(activeHashboards.filter((a) => a !== serial));
    } else {
      setActiveHashboards([...activeHashboards, serial]);
    }
  };

  return (
    <div className={`inline-flex gap-2 py-4 ${className}`}>
      <Button
        size={sizes.compact}
        variant={showAggregate ? variants.secondary : variants.ghost}
        text={"Summary"}
        onClick={() => setShowAggregate(!showAggregate)}
      />
      {series.length > 0 && (
        <Button
          size={sizes.compact}
          variant={
            activeHashboards.length === series.length
              ? variants.secondary
              : variants.ghost
          }
          text={"All Hashboards"}
          onClick={handleAllHashboardsClick}
        />
      )}

      {series.map((s) => (
        <HashboardSelectorItem
          key={s.serial}
          slot={getSlotByHbSn(s.serial)}
          variant={
            activeHashboards.includes(s.serial)
              ? variants.secondary
              : variants.ghost
          }
          onClick={() => {
            handleHashboardClick(s.serial);
          }}
        />
      ))}
    </div>
  );
};

export default HashboardSelector;
