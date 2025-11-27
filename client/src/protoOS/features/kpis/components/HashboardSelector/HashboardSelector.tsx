import clsx from "clsx";
import { getHashboardColor } from "@/protoOS/features/kpis/utility";
import { useMinerStore } from "@/protoOS/store";
import { Circle } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";

import useCssVariable from "@/shared/hooks/useCssVariable";

// TODO: remove this when we update to new API

// Helper to generate consistent button className based on selection state
const getButtonClassName = (isSelected: boolean) =>
  clsx("border-2", {
    "border-core-primary-fill": isSelected,
    "border-transparent": !isSelected,
  });

type HashboardSelectorItemProps = {
  slot?: number;
  selected: boolean;
  onClick: () => void;
};

const HashboardSelectorItem = ({ slot, onClick, selected }: HashboardSelectorItemProps) => {
  const colorVariable = slot ? getHashboardColor(slot) : "";
  const color = useCssVariable(colorVariable);
  const variant = selected ? variants.secondary : variants.ghost;

  return (
    <>
      {slot !== undefined ? (
        <Button
          key={"hashboard-selector-" + slot}
          size={sizes.compact}
          variant={variant}
          className={getButtonClassName(selected)}
          prefixIcon={<Circle className={clsx("mr-1")} width={"w-2"} style={{ background: color }} />}
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
    const activeHashboardLines = activeChartLines.filter((key) => key !== aggregateKey);

    if (activeHashboardLines.length === hashboardLines.length) {
      setActiveChartLines(activeChartLines.filter((key) => hashboardLines.indexOf(key) === -1));
    } else {
      setActiveChartLines([...new Set([...activeChartLines, ...hashboardLines])]);
    }
  };

  const handleHashboardClick = (serial: string) => {
    if (activeChartLines.includes(serial)) {
      setActiveChartLines(activeChartLines.filter((a) => a !== serial));
    } else {
      setActiveChartLines([...activeChartLines, serial]);
    }
  };

  // Check if all hashboards are selected (excluding the aggregate/miner key)
  const hashboardLines = chartLines.filter((key) => key !== aggregateKey);

  // Check if every hashboard in hashboardLines is in activeChartLines
  const allHashboardsSelected =
    hashboardLines.length > 0 && hashboardLines.every((line) => activeChartLines.includes(line));

  const summarySelected = activeChartLines.includes(aggregateKey);

  return (
    <div className={`inline-flex gap-2 py-4 ${className}`}>
      <Button
        size={sizes.compact}
        variant={summarySelected ? variants.secondary : variants.ghost}
        className={getButtonClassName(summarySelected)}
        text={"Summary"}
        onClick={handleSummaryClick}
      />
      {chartLines.length > 0 && (
        <Button
          size={sizes.compact}
          variant={allHashboardsSelected ? variants.secondary : variants.ghost}
          className={getButtonClassName(allHashboardsSelected)}
          text={"All Hashboards"}
          onClick={handleAllHashboardsClick}
        />
      )}

      {hashboardLines.map((serial) => (
        <HashboardSelectorItem
          key={serial}
          slot={useMinerStore.getState().hardware.getHashboard(serial)?.slot}
          selected={activeChartLines.includes(serial)}
          onClick={() => {
            handleHashboardClick(serial);
          }}
        />
      ))}
    </div>
  );
};

export default HashboardSelector;
