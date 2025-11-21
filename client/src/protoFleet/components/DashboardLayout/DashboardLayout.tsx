import useFleet from "@/protoFleet/api/useFleet";
import FleetHealth from "@/protoFleet/features/dashboard/components/FleetHealth";
import SectionHeading from "@/protoFleet/features/dashboard/components/SectionHeading";
import FleetErrors from "@/protoFleet/features/kpis/components/FleetErrors";
import { MinersPage } from "@/protoFleet/features/onboarding";
import { CompleteSetup } from "@/protoFleet/features/onboarding/components/CompleteSetup";
import {
  useDeviceStatusCounts,
  useTotalMiners,
  useDevicePaired,
  useDuration,
  useSetDuration,
} from "@/protoFleet/store";
import DurationSelector from "@/shared/components/DurationSelector";

const DashboardLayout = () => {
  const devicePaired = useDevicePaired();
  useFleet({ scope: "global", mode: "metadata" }); // Ensure fleet data is loaded
  const fleetSize = useTotalMiners();
  const deviceStatusCounts = useDeviceStatusCounts();
  const duration = useDuration();
  const setDuration = useSetDuration();

  return (
    <div className="h-full bg-surface-5">
      {devicePaired ? (
        <div className="flex flex-col">
          <CompleteSetup />

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
          <section className="p-10 pb-6 phone:p-6 tablet:p-6">
            <SectionHeading heading="Performance">
              <DurationSelector duration={duration} onSelect={setDuration} />
            </SectionHeading>
            {/* TODO: Add Performance charts (Hashrate, Uptime, Temperature, Power, Efficiency) */}
            <p className="mt-6 text-300 text-text-primary">
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

export default DashboardLayout;
