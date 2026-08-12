// Container-module "Other hardware" section stories.
//
// Frame 2 groups the PSU and controllers under a single "Other hardware"
// section (vs the rig's separate "PSU" and "Control Board" sections). These
// stories render the REAL production components:
//   - PsuStatusCard        → seeded from the store (in/out voltage + power,
//                            avg/high temp, warning on error)
//   - ControlBoardStatusCard → one card per controller (Latency + CPU capacity)
//
// Live latency and a second controller are backend gaps for container
// modules (tracked in PLANS/PROTO_CONTAINERS_UX_PLAN.md), so the controller
// values here are seeded to preview the full two-controller design — the same
// prototype-first approach used for the hashboard grid.
import { type ReactNode, useEffect } from "react";
import { MemoryRouter } from "react-router-dom";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { type ControllerConfig, OtherHardwareSection } from "./DiagnosticView";
import useMinerStore from "@/protoOS/store/useMinerStore";

// One PSU per container module (design shows a single "Other hardware" PSU).
const PSU_SLOT = 1;

const SeedStore = ({ children }: { children: ReactNode }) => {
  useEffect(() => {
    const store = useMinerStore.getState();

    // Mark this device as a container module (model "CU1" -> useDeviceType).
    store.systemInfo.setSystemInfo({
      manufacturer: "Proto",
      model: "CU1",
      product_name: "Proto CU1",
      cb_sn: "CU1-CB-0001",
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

    const series = <U extends "V" | "W" | "C">(units: U) => ({
      units,
      values: [],
      startTime: Date.now(),
      endTime: Date.now(),
    });
    store.telemetry.updatePsuTelemetry(PSU_SLOT, {
      inputVoltage: { latest: { value: 220.0, units: "V" }, timeSeries: series("V") },
      outputVoltage: { latest: { value: 12.5, units: "V" }, timeSeries: series("V") },
      inputPower: { latest: { value: 3200, units: "W" }, timeSeries: series("W") },
      outputPower: { latest: { value: 3000, units: "W" }, timeSeries: series("W") },
      temperatureAverage: { latest: { value: 48.0, units: "C" }, timeSeries: series("C") },
      temperatureHotspot: { latest: { value: 55.0, units: "C" }, timeSeries: series("C") },
    });

    // A control board so the production single-controller fallback would also
    // resolve; stories still pass explicit `controllers` to show both.
    store.hardware.setControlBoard({ serial: "CU1-CB-0001", boardId: "1" });

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

// Two controllers: Controller 1 healthy, Controller 2 warm with a warning —
// exercises Latency + CPU capacity and the warning (Alert) icon.
const CONTROLLERS: ControllerConfig[] = [
  { title: "Controller 1", latency: 2.3, cpuCapacity: 41.0 },
  { title: "Controller 2", latency: 18.6, cpuCapacity: 92.4, hasWarning: true },
];

// The section framed in the real Diagnostics page chrome (page title + the
// grouped "Other hardware" ComponentSection with PSU + two controller cards).
export const Default: Story = {
  render: () => (
    <div className="min-h-screen bg-surface-base p-8">
      <div className="mx-auto max-w-[1100px]">
        {/* Mirror of DiagnosticView's page header */}
        <div className="flex flex-col items-start gap-3 pb-6 tablet:flex-row tablet:items-center">
          <div className="grow text-heading-300">Diagnostics</div>
        </div>
        <OtherHardwareSection controllers={CONTROLLERS} />
      </div>
    </div>
  ),
};
