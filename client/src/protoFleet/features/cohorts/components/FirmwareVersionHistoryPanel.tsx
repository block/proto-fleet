import { useMemo } from "react";

import { buildFirmwareSeries, type FirmwareSeries } from "./firmwareVersionHistory";
import type { GetCohortFirmwareVersionHistoryResponse } from "@/protoFleet/api/generated/cohort/v1/cohort_pb";
import ChartWidget from "@/protoFleet/features/dashboard/components/ChartWidget";
import { SegmentedMetricPanel } from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel";
import type { SegmentConfig, StatusCount } from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel/types";
import type { FleetDuration } from "@/shared/components/DurationSelector";
import SkeletonBar from "@/shared/components/SkeletonBar";

const buildChartData = (history: GetCohortFirmwareVersionHistoryResponse, series: FirmwareSeries[]): StatusCount[] => {
  return history.points.flatMap((point) => {
    if (!point.timestamp) return [];
    const counts = new Map(point.versions.map((version) => [version.firmwareVersion, version.deviceCount]));
    const chartPoint: StatusCount = { timestamp: point.timestamp };
    for (const item of series) {
      chartPoint[`${item.key}Count`] = item.versions.reduce((total, version) => total + (counts.get(version) ?? 0), 0);
    }
    return [chartPoint];
  });
};

const buildSegmentConfig = (series: FirmwareSeries[]): SegmentConfig =>
  series.reduce<SegmentConfig>((config, item, index) => {
    config[item.key] = {
      color: item.color,
      label: item.label,
      displayInBreakdown: true,
      index,
      showButton: true,
    };
    return config;
  }, {});

type FirmwareVersionHistoryPanelProps = {
  title?: string;
  headline?: string;
  duration: FleetDuration;
  history: GetCohortFirmwareVersionHistoryResponse | null;
  isLoading: boolean;
  hasError: boolean;
};

const FirmwareVersionHistoryPanel = ({
  title = "Firmware versions",
  headline,
  duration,
  history,
  isLoading,
  hasError,
}: FirmwareVersionHistoryPanelProps) => {
  const series = useMemo(() => (history ? buildFirmwareSeries(history) : []), [history]);
  const chartData = useMemo(() => (history ? buildChartData(history, series) : []), [history, series]);
  const segmentConfig = useMemo(() => buildSegmentConfig(series), [series]);

  let panel;
  if (isLoading) {
    panel = (
      <ChartWidget stats={{ label: title, value: undefined }}>
        <SkeletonBar className="h-60 w-full" />
      </ChartWidget>
    );
  } else if (hasError) {
    panel = <ChartWidget stats={{ label: title, value: "Couldn't load firmware history" }}>{null}</ChartWidget>;
  } else if (!history || chartData.length === 0) {
    panel = <ChartWidget stats={{ label: title, value: "No data" }}>{null}</ChartWidget>;
  } else {
    panel = (
      <SegmentedMetricPanel
        title={title}
        headline={headline ?? `${history.memberCount} ${history.memberCount === 1 ? "miner" : "miners"}`}
        chartData={chartData}
        segmentConfig={segmentConfig}
        duration={duration}
      />
    );
  }

  return <div data-testid="firmware-version-history-panel">{panel}</div>;
};

export default FirmwareVersionHistoryPanel;
