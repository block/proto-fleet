// Module PSU hardware. The shared container controllers are tracked separately in #88.
import { type ReactNode, useEffect } from "react";
import { MemoryRouter } from "react-router-dom";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { OtherHardwareSection } from "./DiagnosticView";
import { asDeviceId } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

// One PSU per container module (design shows a single "Other hardware" PSU).
const PSU_SLOT = 1;

const SeedStore = ({ children }: { children: ReactNode }) => {
  useEffect(() => {
    const store = useMinerStore.getState();

    // Mark this device as a container module (model "CU1" -> useDeviceType).
    store.systemInfo.setSystemInfo({
      identity: {
        deviceId: asDeviceId("device:CU1-CB-0001"),
        deviceType: "container",
        adapterId: "mdk-v1",
        manufacturer: "Proto",
        model: "CU1",
        productName: "Proto CU1",
        serialNumber: "CU1-CB-0001",
        macAddresses: [],
      },
      moduleModel: "ct1",
    });

    // PSU hardware + telemetry (mirrors PsuStatusCard.stories seeding).
    store.hardware.addPsu({
      id: PSU_SLOT,
      slot: PSU_SLOT,
      serial: "PSU-000001",
      manufacturer: "Murata Power Solutions",
      model: "D3K3-W-3000-12-HC4C5",
      hwRevision: "v2.1",
      firmware: { appVersion: "2.1.5", bootloaderVersion: "1.2.0" },
    });

    const series = <U extends "V" | "W" | "°C">(units: U) => ({
      units,
      points: [],
    });
    store.telemetry.updatePsuTelemetry(PSU_SLOT, {
      inputVoltage: { latest: { value: 220.0, units: "V" }, timeSeries: series("V") },
      outputVoltage: { latest: { value: 12.5, units: "V" }, timeSeries: series("V") },
      inputPower: { latest: { value: 3200, units: "W" }, timeSeries: series("W") },
      outputPower: { latest: { value: 3000, units: "W" }, timeSeries: series("W") },
      averageTemperature: { latest: { value: 48.0, units: "°C" }, timeSeries: series("°C") },
      hotspotTemperature: { latest: { value: 55.0, units: "°C" }, timeSeries: series("°C") },
    });

    // Clear the seeded module (and its Proto flag) so sibling rig stories start clean.
    return () => {
      useMinerStore.getState().resetDeviceData();
    };
  }, []);

  return <>{children}</>;
};

const meta: Meta = {
  title: "Proto OS/Diagnostic/Other Hardware",
  decorators: [
    (Story) => (
      <MemoryRouter>
        <SeedStore>
          <Story />
        </SeedStore>
      </MemoryRouter>
    ),
  ],
  parameters: { withRouter: false, layout: "fullscreen" },
};

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  render: () => (
    <div className="min-h-screen bg-surface-base p-8">
      <div className="mx-auto max-w-[1100px]">
        {/* Mirror of DiagnosticView's page header */}
        <div className="flex flex-col items-start gap-3 pb-6 tablet:flex-row tablet:items-center">
          <div className="grow text-heading-300">Systems</div>
        </div>
        <OtherHardwareSection />
      </div>
    </div>
  ),
};
