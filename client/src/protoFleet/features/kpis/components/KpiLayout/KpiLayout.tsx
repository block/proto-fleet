import { ReactNode, useEffect, useMemo, useState } from "react";
import { Outlet } from "react-router-dom";

import {
  AggregationType,
  MeasurementType,
} from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useStreamingTelemetryMetrics } from "@/protoFleet/api/useStreamingTelemetryMetrics";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";
import TabMenu from "@/protoFleet/features/kpis/components/TabMenu";
import { KpiOutletContext } from "@/protoFleet/features/kpis/types";
import {
  calculateAggregateStats,
  getLatestAggregateValue,
  mergeStreamingDataPoint,
  transformCombinedMetricsToTimeSeries,
} from "@/protoFleet/features/kpis/utils/telemetryTransforms";
import DurationSelector, {
  Duration,
  durations,
} from "@/shared/components/DurationSelector";
import ProgressCircular from "@/shared/components/ProgressCircular";
import NoPoolsCallout from "@/shared/features/kpis/components/NoPoolsCallout";
import {
  AggregateStats,
  TimeSeriesDataPoint,
} from "@/shared/features/kpis/types";
import { useLocalStorage } from "@/shared/hooks/useLocalStorage";
interface KpiLayoutProps {
  children?: ReactNode;
  title: string;
  duration: Duration;
  setDuration: (duration: Duration) => void;
  outletContext?: KpiOutletContext | null;
  noPoolsLive: boolean;
  hasPoolsConfigured: boolean;
  tabMenuProps: {
    hashrate?: number;
    efficiency?: number;
    powerUsage?: number;
    uptime?: number;
  };
}

const KpiLayout = ({
  children,
  title,
  duration,
  setDuration,
  outletContext,
  noPoolsLive,
  hasPoolsConfigured,
  tabMenuProps,
}: KpiLayoutProps) => {
  return (
    <div className="px-14 pt-14 phone:px-6 phone:pt-6 tablet:px-10 tablet:pt-10">
      {children}

      {noPoolsLive && (
        <NoPoolsCallout arePoolsConfigured={hasPoolsConfigured} />
      )}

      <div className="relative mb-4 flex h-[calc(100vh-theme(spacing.36))] min-h-[800px] flex-col phone:min-h-[1000px]">
        <div className="flex items-center pb-6">
          <div className="grow text-heading-300">{title}</div>
          <DurationSelector
            className="h-fit"
            duration={duration}
            onSelect={setDuration}
          />
        </div>

        <div className="pb-6 phone:pb-6">
          <TabMenu
            hashrate={tabMenuProps.hashrate}
            efficiency={tabMenuProps.efficiency}
            powerUsage={tabMenuProps.powerUsage}
            uptime={tabMenuProps.uptime}
          />
        </div>

        {outletContext ? (
          <Outlet context={outletContext} />
        ) : (
          <div className="flex h-full flex-1 items-center justify-center">
            <ProgressCircular indeterminate />
          </div>
        )}
      </div>
    </div>
  );
};

interface KpiLayoutWrapperProps {
  children?: ReactNode;
}

const KpiLayoutWrapper = ({ children }: KpiLayoutWrapperProps) => {
  const { getItem, setItem } = useLocalStorage();
  const [duration, setDuration] = useState<Duration>(
    getItem("duration") || durations[0],
  );
  const [outletContext, setOutletContext] = useState<KpiOutletContext | null>(
    null,
  );

  // For fleet-level KPI pages, we want to request data for all devices in the org
  // rather than just the specific miners loaded in the fleet store

  const measurementTypes = useMemo(
    () => [
      MeasurementType.HASHRATE,
      MeasurementType.POWER,
      MeasurementType.TEMPERATURE,
      MeasurementType.EFFICIENCY,
    ],
    [],
  );

  const aggregations = useMemo(
    () => [
      AggregationType.AVERAGE,
      AggregationType.MIN,
      AggregationType.MAX,
      AggregationType.SUM,
    ],
    [],
  );

  const streamingOptions = useMemo(
    () => ({
      deviceIds: [], // Empty array will trigger "all devices" selector
      measurementTypes: measurementTypes,
      aggregations: aggregations,
      enabled: true, // Always enabled since we're requesting all devices
    }),
    [measurementTypes, aggregations],
  );

  // Fetch historical combined metrics for all devices
  const {
    data: combinedMetricsData,
    isLoading: isLoadingMetrics,
    error: metricsError,
  } = useTelemetryMetrics({
    // Don't pass deviceIds to request all devices in the organization
    measurementTypes,
    aggregations,
    duration,
    enabled: true, // Always enabled since we're requesting all devices
  });

  // Stream real-time updates for all devices
  const { latestData: streamingData } =
    useStreamingTelemetryMetrics(streamingOptions);

  // Transform data when available
  useEffect(() => {
    if (!combinedMetricsData && !metricsError) return;

    let hashrateTimeSeries: TimeSeriesDataPoint[] = [];
    let hashrateAggregates: AggregateStats = { avg: 0, max: 0, min: 0 };
    let powerTimeSeries: TimeSeriesDataPoint[] = [];
    let powerAggregates: AggregateStats = { avg: 0, max: 0, min: 0 };
    let efficiencyTimeSeries: TimeSeriesDataPoint[] = [];
    let efficiencyAggregates: AggregateStats = { avg: 0, max: 0, min: 0 };
    let temperatureTimeSeries: TimeSeriesDataPoint[] = [];
    let temperatureAggregates: AggregateStats = { avg: 0, max: 0, min: 0 };

    if (combinedMetricsData) {
      // Transform hashrate data
      hashrateTimeSeries = transformCombinedMetricsToTimeSeries(
        combinedMetricsData,
        MeasurementType.HASHRATE,
      );
      hashrateAggregates = calculateAggregateStats(
        combinedMetricsData,
        MeasurementType.HASHRATE,
      );

      // Transform power data
      powerTimeSeries = transformCombinedMetricsToTimeSeries(
        combinedMetricsData,
        MeasurementType.POWER,
      );
      powerAggregates = calculateAggregateStats(
        combinedMetricsData,
        MeasurementType.POWER,
      );

      // Transform efficiency data
      efficiencyTimeSeries = transformCombinedMetricsToTimeSeries(
        combinedMetricsData,
        MeasurementType.EFFICIENCY,
      );
      efficiencyAggregates = calculateAggregateStats(
        combinedMetricsData,
        MeasurementType.EFFICIENCY,
      );

      // Transform temperature data (for uptime placeholder)
      temperatureTimeSeries = transformCombinedMetricsToTimeSeries(
        combinedMetricsData,
        MeasurementType.TEMPERATURE,
      );
      temperatureAggregates = calculateAggregateStats(
        combinedMetricsData,
        MeasurementType.TEMPERATURE,
      );
    }

    setOutletContext({
      duration,
      minerHashrate: {
        hashrate: hashrateTimeSeries,
        aggregates: hashrateAggregates,
      },
      minerEfficiency: {
        efficiency: efficiencyTimeSeries,
        aggregates: efficiencyAggregates,
      },
      minerPowerUsage: {
        powerUsage: powerTimeSeries,
        aggregates: powerAggregates,
      },
      minerUptime: {
        uptime: temperatureTimeSeries, // Placeholder until uptime is available
        aggregates: temperatureAggregates,
      },
    });
  }, [combinedMetricsData, duration, metricsError]);

  // Update with streaming data
  useEffect(() => {
    if (!streamingData) return;

    try {
      // Get latest values from streaming data
      const latestHashrate = getLatestAggregateValue(
        streamingData,
        MeasurementType.HASHRATE,
      );
      const latestPower = getLatestAggregateValue(
        streamingData,
        MeasurementType.POWER,
      );
      const latestEfficiency = getLatestAggregateValue(
        streamingData,
        MeasurementType.EFFICIENCY,
      );
      const latestTemperature = getLatestAggregateValue(
        streamingData,
        MeasurementType.TEMPERATURE,
      );

      // Create new data points
      const now = Date.now();
      const newHashratePoint = { datetime: now, value: latestHashrate };
      const newPowerPoint = { datetime: now, value: latestPower };
      const newEfficiencyPoint = { datetime: now, value: latestEfficiency };
      const newTemperaturePoint = { datetime: now, value: latestTemperature };

      // Use functional update to access current state without dependency
      setOutletContext((prev) => {
        if (!prev) return null;

        // Merge with existing data using previous state
        const updatedHashrateData = mergeStreamingDataPoint(
          prev.minerHashrate.hashrate,
          newHashratePoint,
        );
        const updatedPowerData = mergeStreamingDataPoint(
          prev.minerPowerUsage.powerUsage,
          newPowerPoint,
        );
        const updatedEfficiencyData = mergeStreamingDataPoint(
          prev.minerEfficiency.efficiency,
          newEfficiencyPoint,
        );
        const updatedTemperatureData = mergeStreamingDataPoint(
          prev.minerUptime.uptime,
          newTemperaturePoint,
        );

        return {
          ...prev,
          minerHashrate: {
            ...prev.minerHashrate,
            hashrate: updatedHashrateData,
          },
          minerPowerUsage: {
            ...prev.minerPowerUsage,
            powerUsage: updatedPowerData,
          },
          minerEfficiency: {
            ...prev.minerEfficiency,
            efficiency: updatedEfficiencyData,
          },
          minerUptime: {
            ...prev.minerUptime,
            uptime: updatedTemperatureData,
          },
        };
      });
    } catch (error) {
      console.error("Error processing streaming data:", error);
    }
  }, [streamingData]);

  // Set the duration in local storage when it changes
  const handleDurationChange = (newDuration: Duration) => {
    setItem("duration", newDuration);
    setDuration(newDuration);
  };

  // Get current values for the tab menu
  const currentHashrateValue =
    outletContext?.minerHashrate.aggregates?.avg || 0;
  const currentEfficiencyValue =
    outletContext?.minerEfficiency.aggregates?.avg || 0;
  const currentPowerUsageValue =
    outletContext?.minerPowerUsage.aggregates?.avg || 0;
  const currentUptimeValue = outletContext?.minerUptime.aggregates?.avg || 0;

  // Mock pool status - TODO: Replace with real pool status
  const poolsLive = true;
  const poolsConfigured = true;

  // Show loading state while fetching initial data
  const isLoading = isLoadingMetrics && !outletContext;

  return (
    <KpiLayout
      title="Fleet performance"
      duration={duration}
      setDuration={handleDurationChange}
      outletContext={isLoading ? null : outletContext}
      noPoolsLive={!poolsLive}
      hasPoolsConfigured={poolsConfigured}
      tabMenuProps={{
        hashrate: currentHashrateValue,
        efficiency: currentEfficiencyValue,
        powerUsage: currentPowerUsageValue,
        uptime: currentUptimeValue,
      }}
    >
      {children}
    </KpiLayout>
  );
};

export default KpiLayoutWrapper;
