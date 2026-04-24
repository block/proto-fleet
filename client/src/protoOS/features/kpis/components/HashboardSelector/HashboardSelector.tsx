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
          testId={`chart-filter-hashboard-${slot}`}
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
    // If no filters are active (default state), selecting Summary enters filtered mode
    if (activeChartLines.length === 0) {
      setActiveChartLines([aggregateKey]);
      return;
    }

    // In filtered mode, toggle Summary on/off
    if (activeChartLines.includes(aggregateKey)) {
      const newLines = activeChartLines.filter((a) => a !== aggregateKey);
      // If this was the last active line, return to default (show all)
      setActiveChartLines(newLines.length === 0 ? [] : newLines);
    } else {
      setActiveChartLines([...activeChartLines, aggregateKey]);
    }
  };

  const handleAllHashboardsClick = () => {
    const hashboardLines = chartLines.filter((key) => key !== aggregateKey);

    // If no filters are active (default state), selecting All Hashboards enters filtered mode
    if (activeChartLines.length === 0) {
      setActiveChartLines([...hashboardLines]);
      return;
    }

    const activeHashboardLines = activeChartLines.filter((key) => key !== aggregateKey);

    // Check if all hashboards are actually selected (not just matching count)
    const areAllHashboardsSelected =
      hashboardLines.length > 0 && hashboardLines.every((line) => activeHashboardLines.includes(line));

    if (areAllHashboardsSelected) {
      const summaryOnly = activeChartLines.filter((key) => key === aggregateKey);
      // If Summary is the only remaining line, return to default (show all)
      setActiveChartLines(summaryOnly.length === 0 ? [] : summaryOnly);
    } else {
      // Select all hashboards (preserve Summary state)
      const summaryState = activeChartLines.filter((key) => key === aggregateKey);
      setActiveChartLines([...summaryState, ...hashboardLines]);
    }
  };

  const handleHashboardClick = (serial: string) => {
    // If no filters are active (default state), selecting a hashboard enters filtered mode
    if (activeChartLines.length === 0) {
      setActiveChartLines([serial]);
      return;
    }

    // In filtered mode, toggle hashboard on/off
    if (activeChartLines.includes(serial)) {
      const newLines = activeChartLines.filter((a) => a !== serial);
      // If this was the last active line, return to default (show all)
      setActiveChartLines(newLines.length === 0 ? [] : newLines);
    } else {
      setActiveChartLines([...activeChartLines, serial]);
    }
  };

  // Check if all hashboards are selected (excluding the aggregate/miner key)
  const hashboardLines = chartLines.filter((key) => key !== aggregateKey);

  // When no filters are active (default state), no buttons should be selected
  // In filtered mode, buttons are selected based on activeChartLines
  const isFilteredMode = activeChartLines.length > 0;

  // Check if every hashboard in hashboardLines is in activeChartLines
  const allHashboardsSelected =
    isFilteredMode && hashboardLines.length > 0 && hashboardLines.every((line) => activeChartLines.includes(line));

  const summarySelected = isFilteredMode && activeChartLines.includes(aggregateKey);

  return (
    <div className={`inline-flex gap-2 py-4 ${className}`}>
      <Button
        size={sizes.compact}
        variant={summarySelected ? variants.secondary : variants.ghost}
        className={getButtonClassName(summarySelected)}
        text={"Summary"}
        onClick={handleSummaryClick}
        testId="chart-filter-summary"
      />
      {hashboardLines.length > 0 ? (
        <Button
          size={sizes.compact}
          variant={allHashboardsSelected ? variants.secondary : variants.ghost}
          className={getButtonClassName(allHashboardsSelected)}
          text={"All Hashboards"}
          onClick={handleAllHashboardsClick}
          testId="chart-filter-all-hashboards"
        />
      ) : null}

      {hashboardLines.map((serial) => (
        <HashboardSelectorItem
          key={serial}
          slot={useMinerStore.getState().hardware.getHashboard(serial)?.slot}
          selected={isFilteredMode ? activeChartLines.includes(serial) : false}
          onClick={() => {
            handleHashboardClick(serial);
          }}
        />
      ))}
    </div>
  );
};

export default HashboardSelector;
