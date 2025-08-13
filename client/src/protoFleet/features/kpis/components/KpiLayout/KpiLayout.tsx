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
  processAllMetrics,
  processAllMetricsWithStreaming,
} from "@/protoFleet/features/kpis/utils/telemetryTransforms";
import DurationSelector, {
  Duration,
  durations,
} from "@/shared/components/DurationSelector";
import ProgressCircular from "@/shared/components/ProgressCircular";
import NoPoolsCallout from "@/shared/features/kpis/components/NoPoolsCallout";
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
    temperature?: number;
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
            temperature={tabMenuProps.temperature}
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

    const processedMetrics = processAllMetrics(combinedMetricsData, duration);

    setOutletContext({
      duration,
      minerHashrate: {
        hashrate: processedMetrics.hashrate.timeSeries,
        aggregates: processedMetrics.hashrate.aggregates,
      },
      minerEfficiency: {
        efficiency: processedMetrics.efficiency.timeSeries,
        aggregates: processedMetrics.efficiency.aggregates,
      },
      minerPowerUsage: {
        powerUsage: processedMetrics.power.timeSeries,
        aggregates: processedMetrics.power.aggregates,
      },
      minerTemperature: {
        temperature: processedMetrics.temperature.timeSeries,
        aggregates: processedMetrics.temperature.aggregates,
      },
    });
  }, [combinedMetricsData, duration, metricsError]);

  // Update with streaming data
  useEffect(() => {
    if (!streamingData || !combinedMetricsData) return;

    // eslint-disable-next-line no-console
    console.log("Streaming values:", {
      hashrate:
        streamingData.metrics
          .find((m) => m.measurementType === MeasurementType.HASHRATE)
          ?.aggregatedValues.find(
            (av) => av.aggregationType === AggregationType.SUM,
          )?.value || 0,
      efficiency:
        streamingData.metrics
          .find((m) => m.measurementType === MeasurementType.EFFICIENCY)
          ?.aggregatedValues.find(
            (av) => av.aggregationType === AggregationType.AVERAGE,
          )?.value || 0,
      power:
        streamingData.metrics
          .find((m) => m.measurementType === MeasurementType.POWER)
          ?.aggregatedValues.find(
            (av) => av.aggregationType === AggregationType.SUM,
          )?.value || 0,
      temperature:
        streamingData.metrics
          .find((m) => m.measurementType === MeasurementType.TEMPERATURE)
          ?.aggregatedValues.find(
            (av) => av.aggregationType === AggregationType.AVERAGE,
          )?.value || 0,
    });

    try {
      // Reprocess all data with streaming updates merged in
      const processedMetrics = processAllMetricsWithStreaming(
        combinedMetricsData,
        streamingData,
        duration,
      );

      setOutletContext({
        duration,
        minerHashrate: {
          hashrate: processedMetrics.hashrate.timeSeries,
          aggregates: processedMetrics.hashrate.aggregates,
        },
        minerEfficiency: {
          efficiency: processedMetrics.efficiency.timeSeries,
          aggregates: processedMetrics.efficiency.aggregates,
        },
        minerPowerUsage: {
          powerUsage: processedMetrics.power.timeSeries,
          aggregates: processedMetrics.power.aggregates,
        },
        minerTemperature: {
          temperature: processedMetrics.temperature.timeSeries,
          aggregates: processedMetrics.temperature.aggregates,
        },
      });
    } catch (error) {
      console.error("Error processing streaming data:", error);
    }
  }, [streamingData, combinedMetricsData, duration]);

  // Set the duration in local storage when it changes
  const handleDurationChange = (newDuration: Duration) => {
    setItem("duration", newDuration);
    setDuration(newDuration);
  };

  // Get current values for the tab menu
  const currentHashrateValue =
    outletContext?.minerHashrate.hashrate.slice(-1)[0]?.value || 0;
  const currentEfficiencyValue =
    outletContext?.minerEfficiency.efficiency.slice(-1)[0]?.value || 0;
  const currentPowerUsageValue =
    outletContext?.minerPowerUsage.powerUsage.slice(-1)[0]?.value || 0;
  const currentTemperatureValue =
    outletContext?.minerTemperature.temperature.slice(-1)[0]?.value || 0;

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
        temperature: currentTemperatureValue,
      }}
    >
      {children}
    </KpiLayout>
  );
};

export default KpiLayoutWrapper;
