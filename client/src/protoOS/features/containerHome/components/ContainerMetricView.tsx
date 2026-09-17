import type { ReactNode } from "react";
import type { CategorySummary, HomeChartMode, HomeHistory, HomeReading, LocationReading } from "../types";
import { HomeHistoryChart, LocationBarChart } from "./HomeCharts";
import Button from "@/shared/components/Button";
import Row from "@/shared/components/Row";
import Stat from "@/shared/components/Stat";

export function HomeSummary({ readings, children }: { readings: HomeReading[]; children?: ReactNode }) {
  return (
    <section aria-label="Live readings" className="rounded-3xl bg-surface-elevated-base p-6 shadow-100">
      <div className="grid grid-cols-2 gap-x-4 gap-y-6 tablet:grid-cols-3 desktop:grid-cols-6">
        {readings.map((reading) => (
          <Stat key={reading.id} label={reading.label} value={reading.value} size="small" />
        ))}
      </div>
      {children}
    </section>
  );
}

interface Props {
  title: string;
  readings: HomeReading[];
  history: HomeHistory;
  mode?: HomeChartMode;
  modes?: { id: HomeChartMode; label: string }[];
  onModeChange?: (mode: HomeChartMode) => void;
  categories?: CategorySummary[];
  locations?: LocationReading[];
}

export default function ContainerMetricView({
  title,
  readings,
  history,
  mode,
  modes = [],
  onModeChange,
  categories = [],
  locations,
}: Props) {
  return (
    <div className="flex flex-col gap-1">
      <h1 className="sr-only">{title}</h1>
      <HomeSummary readings={readings} />
      <section aria-label={`${title} chart`} className="rounded-3xl bg-surface-elevated-base p-6 shadow-100">
        <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-200 text-text-primary-50">{title === "Power" ? "Power distribution" : title}</h2>
          <ChartModeSelector title={title} modes={modes} mode={mode} onModeChange={onModeChange} />
        </div>
        <MetricChart mode={mode} locations={locations} history={history} />
      </section>
      {categories.length ? (
        <div className="grid grid-cols-1 gap-1 tablet:grid-cols-2 desktop:grid-cols-4">
          {categories.map((category) => (
            <CategoryStatisticsCard key={category.id} category={category} />
          ))}
        </div>
      ) : null}
    </div>
  );
}

function ChartModeSelector({
  title,
  modes = [],
  mode,
  onModeChange,
}: Pick<Props, "title" | "modes" | "mode" | "onModeChange">) {
  if (!modes.length) return null;
  return (
    <div role="group" aria-label={`${title} chart mode`} className="flex gap-1">
      {modes.map((option) => (
        <Button
          key={option.id}
          text={option.label}
          size="compact"
          variant={mode === option.id ? "primary" : "secondary"}
          ariaPressed={mode === option.id}
          onClick={() => onModeChange?.(option.id)}
        />
      ))}
    </div>
  );
}

function MetricChart({ mode, locations, history }: Pick<Props, "mode" | "locations" | "history">) {
  if (mode === "fluid" && locations) return <LocationBarChart readings={locations} />;
  return <HomeHistoryChart history={history} />;
}

function CategoryStatisticsCard({ category }: { category: CategorySummary }) {
  const statistics = [
    ["High", category.high],
    ["Low", category.low],
    ["Average", category.average],
  ] as const;
  return (
    <section
      aria-label={`${category.label} period statistics`}
      className="rounded-xl bg-surface-elevated-base p-6 shadow-100"
    >
      <h2 className="mb-3 text-200 text-text-primary-50">{category.label}</h2>
      {statistics.map(([label, value], index) => (
        <Row key={label} compact divider={index < 2}>
          <div className="flex justify-between gap-4 text-200">
            <span>{label}</span>
            <span className="tabular-nums">{value}</span>
          </div>
        </Row>
      ))}
    </section>
  );
}
