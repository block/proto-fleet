import { useMemo } from "react";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import useFleet from "@/protoFleet/api/useFleet";
import { useStreamingTelemetryMetrics } from "@/protoFleet/api/useStreamingTelemetryMetrics";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";
import { EfficiencyPanel } from "@/protoFleet/features/dashboard/components/EfficiencyPanel";
import FleetHealth from "@/protoFleet/features/dashboard/components/FleetHealth";
import { HashratePanel } from "@/protoFleet/features/dashboard/components/HashratePanel";
import { PowerPanel } from "@/protoFleet/features/dashboard/components/PowerPanel";
import SectionHeading from "@/protoFleet/features/dashboard/components/SectionHeading";
import { TemperaturePanel } from "@/protoFleet/features/dashboard/components/TemperaturePanel";
import FleetErrors from "@/protoFleet/features/kpis/components/FleetErrors";
import { MinersPage } from "@/protoFleet/features/onboarding";
import { CompleteSetup } from "@/protoFleet/features/onboarding/components/CompleteSetup";
import {
  useDevicePaired,
  useDeviceStatusCounts,
  useDuration,
  useSetDuration,
  useTemperatureStatusCounts,
  useTotalMiners,
} from "@/protoFleet/store";
import DurationSelector from "@/shared/components/DurationSelector";

const Dashboard = () => {
  const devicePaired = useDevicePaired();
  useFleet({
    scope: "global",
    mode: "metadata",
    pageSize: 1,
    pairingStatuses: [
      PairingStatus.PAIRED,
      PairingStatus.AUTHENTICATION_NEEDED,
    ],
  }); // Ensure fleet data is loaded
  const fleetSize = useTotalMiners();
  const deviceStatusCounts = useDeviceStatusCounts();
  const temperatureStatusCounts = useTemperatureStatusCounts();
  const duration = useDuration();
  const setDuration = useSetDuration();

  // Memoize the telemetry options to prevent re-renders
  const telemetryOptions = useMemo(
    () => ({
      measurementTypes: [MeasurementType.TEMPERATURE],
      duration: duration,
      enabled: true,
    }),
    [duration], // Only recreate when duration changes
  );

  // Fetch initial telemetry metrics including temperature status
  const { isLoading } = useTelemetryMetrics(telemetryOptions);

  // Memoize streaming options
  const streamingOptions = useMemo(
    () => ({
      deviceIds: [], // Empty means all devices
      measurementTypes: [MeasurementType.TEMPERATURE],
      enabled: true,
    }),
    [], // Static options, no dependencies
  );

  // Enable streaming updates for temperature status
  useStreamingTelemetryMetrics(streamingOptions);

  return (
    <div className="h-full">
      {devicePaired ? (
        <div className="flex flex-col">
          <CompleteSetup className="p-10 phone:p-6 tablet:p-6" />

          {/* Overview Section */}
          <section className="p-10 phone:p-6 tablet:p-6">
            <SectionHeading heading="Overview" />
            <div className="mt-6 flex flex-col gap-1">
              {/* TODO: Get error counts from API */}
              <FleetErrors
                controlBoardErrors={0}
                fanErrors={0}
                hashboardErrors={0}
                psuErrors={0}
              />
              <FleetHealth
                fleetSize={fleetSize ?? 1} // prevent division by zero
                healthyMiners={deviceStatusCounts?.hashingCount ?? 0}
                offlineMiners={deviceStatusCounts?.offlineCount ?? 0}
                unhealthyMiners={
                  (deviceStatusCounts?.sleepingCount ?? 0) +
                  (deviceStatusCounts?.brokenCount ?? 0)
                }
              />
            </div>
          </section>

          {/* Performance Section */}
          <section className="flex flex-col gap-6 p-10 pb-6 phone:p-6 tablet:p-6">
            <SectionHeading heading="Performance">
              <DurationSelector duration={duration} onSelect={setDuration} />
            </SectionHeading>

            <div className="flex flex-col gap-1">
              {/* Hashrate Panel - shows fleet hashrate over time */}
              <HashratePanel duration={duration} />

              {/* Temperature Panel - shows temperature status distribution */}
              <TemperaturePanel
                temperatureStatusCounts={temperatureStatusCounts}
                isLoading={isLoading}
              />

              {/* Power and Efficiency Panels - side by side */}
              <div className="grid grid-cols-2 gap-1 phone:grid-cols-1 tablet:grid-cols-1">
                {/* Power Panel - shows fleet power consumption over time */}
                <PowerPanel duration={duration} />

                {/* Efficiency Panel - shows fleet efficiency over time */}
                <EfficiencyPanel duration={duration} />
              </div>
            </div>

            {/* TODO: Add Uptime chart */}
            <p className="text-300 text-text-primary">
              Data gaps may occur where third-party miner telemetry is
              unavailable. Efficiency and power reports will not reflect
              Antminer devices.
            </p>
          </section>

          {/* Privacy Policy */}
          <footer className="px-10 pt-20 pb-6 text-300 phone:px-5 tablet:px-5">
            <p className="text-text-primary">
              Powerful mining tools. Built for decentralization.{" "}
              <span className="text-text-primary-50">
                Proto Fleet v01.2.3 © 2025 Block, Inc.{" "}
                <a
                  href="https://proto.xyz/privacy-policy"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="hover:underline"
                >
                  Privacy Notice
                </a>
              </span>
            </p>
          </footer>
        </div>
      ) : (
        <MinersPage />
      )}
    </div>
  );
};

export default Dashboard;
