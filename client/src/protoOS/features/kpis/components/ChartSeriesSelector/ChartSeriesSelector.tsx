import clsx from "clsx";

import { Circle } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import useCssVariable from "@/shared/hooks/useCssVariable";

export interface ChartSeriesOption {
  key: string;
  label: string;
  color: string;
}

interface ChartSeriesSelectorProps {
  activeKeys: string[];
  aggregateKey: string;
  onChange: (keys: string[]) => void;
  segmentsLabel: string;
  series: ChartSeriesOption[];
  className?: string;
}

const buttonClassName = (selected: boolean) =>
  clsx("border-2", selected ? "border-core-primary-fill" : "border-transparent");

const SeriesButton = ({
  option,
  selected,
  onClick,
  testId,
}: {
  option: ChartSeriesOption;
  selected: boolean;
  onClick: () => void;
  testId: string;
}) => {
  const color = useCssVariable(option.color);
  return (
    <Button
      size={sizes.compact}
      variant={selected ? variants.secondary : variants.ghost}
      ariaPressed={selected}
      className={buttonClassName(selected)}
      prefixIcon={<Circle className="mr-1" width="w-2" style={{ background: color }} />}
      text={option.label}
      onClick={onClick}
      testId={testId}
    />
  );
};

const ChartSeriesSelector = ({
  activeKeys,
  aggregateKey,
  onChange,
  segmentsLabel,
  series,
  className = "",
}: ChartSeriesSelectorProps) => {
  const segmentName = segmentsLabel.toLowerCase();
  const singularSegmentName = segmentName.endsWith("s") ? segmentName.slice(0, -1) : segmentName;
  const filtered = activeKeys.length > 0;
  const aggregateSelected = filtered && activeKeys.includes(aggregateKey);
  const allSeriesSelected = filtered && series.length > 0 && series.every(({ key }) => activeKeys.includes(key));

  const toggleAggregate = () => {
    if (!filtered) {
      onChange([aggregateKey]);
      return;
    }
    const next = aggregateSelected ? activeKeys.filter((key) => key !== aggregateKey) : [...activeKeys, aggregateKey];
    onChange(next.length ? next : []);
  };

  const toggleAllSeries = () => {
    if (!filtered) {
      onChange(series.map(({ key }) => key));
      return;
    }
    const aggregate = activeKeys.filter((key) => key === aggregateKey);
    onChange(allSeriesSelected ? aggregate : [...aggregate, ...series.map(({ key }) => key)]);
  };

  const toggleSeries = (key: string) => {
    if (!filtered) {
      onChange([key]);
      return;
    }
    const next = activeKeys.includes(key) ? activeKeys.filter((activeKey) => activeKey !== key) : [...activeKeys, key];
    onChange(next.length ? next : []);
  };

  return (
    <div className={`inline-flex gap-2 py-4 ${className}`}>
      <Button
        size={sizes.compact}
        variant={aggregateSelected ? variants.secondary : variants.ghost}
        ariaPressed={aggregateSelected}
        className={buttonClassName(aggregateSelected)}
        text="Summary"
        onClick={toggleAggregate}
        testId="chart-filter-summary"
      />
      {series.length > 0 ? (
        <Button
          size={sizes.compact}
          variant={allSeriesSelected ? variants.secondary : variants.ghost}
          ariaPressed={allSeriesSelected}
          className={buttonClassName(allSeriesSelected)}
          text={`All ${segmentsLabel}`}
          onClick={toggleAllSeries}
          testId={`chart-filter-all-${segmentName}`}
        />
      ) : null}
      {series.map((option) => (
        <SeriesButton
          key={option.key}
          option={option}
          selected={filtered ? activeKeys.includes(option.key) : false}
          onClick={() => toggleSeries(option.key)}
          testId={`chart-filter-${singularSegmentName}-${option.label}`}
        />
      ))}
    </div>
  );
};

export default ChartSeriesSelector;
