import { ReactNode, useEffect, useState } from "react";

import { Outlet } from "react-router-dom";
import TabMenu from "@/protoFleet/features/kpis/components/TabMenu";
import { KpiOutletContext } from "@/protoFleet/features/kpis/types";
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

  // Create mock time series data
  const createTimeSeriesData = (): TimeSeriesDataPoint[] => {
    const now = Date.now();
    const hourMs = 3600 * 1000;
    const data: TimeSeriesDataPoint[] = [];

    // Generate 24 hours of data points
    for (let i = 0; i < 24; i++) {
      data.push({
        datetime: now - (24 - i) * hourMs,
        value: 10 + Math.random() * 10, // Random value between 10-20
      });
    }

    return data;
  };

  // Create mock aggregates
  const createMockAggregates = (): AggregateStats => {
    return {
      avg: 15.5,
      max: 20.0,
      min: 10.0,
    };
  };

  // Mock KPI data
  useEffect(() => {
    setOutletContext({
      duration,
      minerHashrate: {
        hashrate: createTimeSeriesData(),
        aggregates: createMockAggregates(),
      },
      minerEfficiency: {
        efficiency: createTimeSeriesData(),
        aggregates: createMockAggregates(),
      },
      minerPowerUsage: {
        powerUsage: createTimeSeriesData(),
        aggregates: createMockAggregates(),
      },
      minerUptime: {
        uptime: createTimeSeriesData(),
        aggregates: createMockAggregates(),
      },
    });
  }, [duration]);

  // Set the duration in local storage when it changes
  const handleDurationChange = (newDuration: Duration) => {
    setItem("duration", newDuration);
    setDuration(newDuration);
  };

  // Mock values for the tab menu
  const mockHashrateValue = 15.8;
  const mockEfficiencyValue = 38.2;
  const mockPowerUsageValue = 0.55;
  const mockUptimeValue = 90;

  // Mock pool status
  const poolsLive = true;
  const poolsConfigured = true;

  return (
    <KpiLayout
      title="Fleet performance"
      duration={duration}
      setDuration={handleDurationChange}
      outletContext={outletContext}
      noPoolsLive={!poolsLive}
      hasPoolsConfigured={poolsConfigured}
      tabMenuProps={{
        hashrate: mockHashrateValue,
        efficiency: mockEfficiencyValue,
        powerUsage: mockPowerUsageValue,
        uptime: mockUptimeValue,
      }}
    >
      {children}
    </KpiLayout>
  );
};

export default KpiLayoutWrapper;
