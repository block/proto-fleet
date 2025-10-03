import clsx from "clsx";
import { getHashboardColor } from "@/protoOS/features/kpis/utility";
import { useMinerStore } from "@/protoOS/store";
import { Circle } from "@/shared/assets/icons";
import Button, {
  type ButtonVariant,
  sizes,
  variants,
} from "@/shared/components/Button";

import useCssVariable from "@/shared/hooks/useCssVariable";

// TODO: remove this when we update to new API

type HashboardSelectorItemProps = {
  slot?: number;
  onClick: () => void;
  variant: ButtonVariant;
};

const HashboardSelectorItem = ({
  slot,
  onClick,
  variant,
}: HashboardSelectorItemProps) => {
  const colorVariable = slot ? getHashboardColor(slot) : "";
  const color = useCssVariable(colorVariable);

  return (
    <>
      {slot !== undefined ? (
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
          text={slot ? slot.toString() : ""}
          onClick={onClick}
        />
      ) : null}
    </>
  );
};

type HashboardSelectorProps = {
  chartLines: string[];
  setActiveChartLines: (serials: string[]) => void;
  activeChartLines: string[];
  aggregateKey: string;
  className?: string;
};

const HashboardSelector = ({
  chartLines,
  setActiveChartLines,
  activeChartLines,
  aggregateKey,
  className = "",
}: HashboardSelectorProps) => {
  const handleSummaryClick = () => {
    if (activeChartLines.includes(aggregateKey)) {
      setActiveChartLines(activeChartLines.filter((a) => a !== aggregateKey));
    } else {
      setActiveChartLines([...activeChartLines, aggregateKey]);
    }
  };

  const handleAllHashboardsClick = () => {
    const hashboardLines = chartLines.filter((key) => key !== aggregateKey);
    const activeHashboardLines = activeChartLines.filter(
      (key) => key !== aggregateKey,
    );

    if (activeHashboardLines.length === hashboardLines.length) {
      setActiveChartLines(
        activeChartLines.filter((key) => hashboardLines.indexOf(key) === -1),
      );
    } else {
      setActiveChartLines([
        ...new Set([...activeChartLines, ...hashboardLines]),
      ]);
    }
  };

  const handleHashboardClick = (serial: string) => {
    if (activeChartLines.includes(serial)) {
      setActiveChartLines(activeChartLines.filter((a) => a !== serial));
    } else {
      setActiveChartLines([...activeChartLines, serial]);
    }
  };

  return (
    <div className={`inline-flex gap-2 py-4 ${className}`}>
      <Button
        size={sizes.compact}
        variant={
          activeChartLines.includes(aggregateKey)
            ? variants.secondary
            : variants.ghost
        }
        text={"Summary"}
        onClick={handleSummaryClick}
      />
      {chartLines.length > 0 && (
        <Button
          size={sizes.compact}
          variant={
            activeChartLines.length === chartLines.length
              ? variants.secondary
              : variants.ghost
          }
          text={"All Hashboards"}
          onClick={handleAllHashboardsClick}
        />
      )}

      {chartLines.map((serial) => (
        <HashboardSelectorItem
          key={serial}
          slot={useMinerStore.getState().hardware.getHashboard(serial)?.slot}
          variant={
            activeChartLines.includes(serial)
              ? variants.secondary
              : variants.ghost
          }
          onClick={() => {
            handleHashboardClick(serial);
          }}
        />
      ))}
    </div>
  );
};

export default HashboardSelector;
