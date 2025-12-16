import { useComponentErrors } from "@/protoFleet/api/useComponentErrors";
import useFleetCounts from "@/protoFleet/api/useFleetCounts";
import { EfficiencyPanel } from "@/protoFleet/features/dashboard/components/EfficiencyPanel";
import FleetHealth from "@/protoFleet/features/dashboard/components/FleetHealth";
import { HashratePanel } from "@/protoFleet/features/dashboard/components/HashratePanel";
import { PowerPanel } from "@/protoFleet/features/dashboard/components/PowerPanel";
import SectionHeading from "@/protoFleet/features/dashboard/components/SectionHeading";
import { TemperaturePanel } from "@/protoFleet/features/dashboard/components/TemperaturePanel";
import { UptimePanel } from "@/protoFleet/features/dashboard/components/UptimePanel";
import FleetErrors from "@/protoFleet/features/kpis/components/FleetErrors";
import { MinersPage } from "@/protoFleet/features/onboarding";
import { CompleteSetup } from "@/protoFleet/features/onboarding/components/CompleteSetup";
import { useDevicePaired, useDuration, useSetDuration } from "@/protoFleet/store";
import DurationSelector from "@/shared/components/DurationSelector";
import { useStickyState } from "@/shared/hooks/useStickyState";
import { buildVersionInfo } from "@/shared/utils/version";

const Dashboard = () => {
  const devicePaired = useDevicePaired();
  const { totalMiners, stateCounts } = useFleetCounts();
  const { controlBoardErrors, fanErrors, hashboardErrors, psuErrors } = useComponentErrors();
  const duration = useDuration();
  const setDuration = useSetDuration();
  const currentYear = new Date().getFullYear();
  const { refs } = useStickyState();

  return (
    <div className="h-full">
      {devicePaired ? (
        <div className="flex flex-col">
          <CompleteSetup className="p-10 phone:p-6 tablet:p-6" />

          {/* Overview Section */}
          <section className="p-10 phone:p-6 tablet:p-6">
            <SectionHeading heading="Overview" />
            <div className="mt-6 flex flex-col gap-1">
              <FleetErrors
                controlBoardErrors={controlBoardErrors}
                fanErrors={fanErrors}
                hashboardErrors={hashboardErrors}
                psuErrors={psuErrors}
              />
              <FleetHealth
                fleetSize={totalMiners || 1} // prevent division by zero
                healthyMiners={stateCounts?.hashingCount ?? 0}
                offlineMiners={stateCounts?.offlineCount ?? 0}
                unhealthyMiners={(stateCounts?.sleepingCount ?? 0) + (stateCounts?.brokenCount ?? 0)}
              />
            </div>
          </section>

          {/* Performance Section */}
          <section className="pb-6">
            <div ref={refs.vertical.start} />
            <div className="sticky top-0 z-2 bg-surface-5 px-10 pt-10 pb-6 dark:bg-surface-base phone:px-6 phone:pt-6 tablet:px-6 tablet:pt-6">
              <SectionHeading heading="Performance">
                <DurationSelector duration={duration} onSelect={setDuration} />
              </SectionHeading>
            </div>

            <div className="flex flex-col gap-1 px-10 phone:px-6 tablet:px-6">
              {/* Hashrate Panel - shows fleet hashrate over time */}
              <HashratePanel duration={duration} />

              {/* Uptime Panel - shows uptime status distribution */}
              <UptimePanel duration={duration} />

              {/* Temperature Panel - shows temperature status distribution */}
              <TemperaturePanel duration={duration} />

              {/* Power and Efficiency Panels - side by side */}
              <div className="grid grid-cols-2 gap-1 phone:grid-cols-1 tablet:grid-cols-1">
                {/* Power Panel - shows fleet power consumption over time */}
                <PowerPanel duration={duration} />

                {/* Efficiency Panel - shows fleet efficiency over time */}
                <EfficiencyPanel duration={duration} />
              </div>
            </div>

            <p className="px-10 pt-6 text-300 text-text-primary phone:px-6 tablet:px-6">
              Data gaps may occur where third-party miner telemetry is unavailable. Efficiency and power reports will
              not reflect Antminer devices.
            </p>
            {/* eslint-disable-next-line react-hooks/refs */}
            <div ref={refs.vertical.end} />
          </section>

          {/* Privacy Policy */}
          <footer className="px-10 pt-20 pb-6 text-300 phone:px-5 tablet:px-5">
            <p className="text-text-primary">
              Powerful mining tools. Built for decentralization.{" "}
              <span className="text-text-primary-50">
                Proto Fleet {buildVersionInfo.version} © {currentYear} Block, Inc.{" "}
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
