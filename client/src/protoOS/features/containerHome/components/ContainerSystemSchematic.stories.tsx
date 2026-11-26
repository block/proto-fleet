import type { Meta, StoryObj } from "@storybook/react";
import ContainerSystemSchematic, { type ContainerSystemSchematicProps } from "./ContainerSystemSchematic";

interface SchematicStoryArgs {
  tankALevelPercent: number;
  tankBLevelPercent: number;
  tankATemperatureC: number;
  tankBTemperatureC: number;
  tankAPressurePsi: number;
  tankBPressurePsi: number;
  fanSpeedPercent: number;
  criticalFan: number;
  criticalFanSpeedPercent: number;
  flowRateGpm: number;
  flowAlert: boolean;
  pumpSpeedHz: number;
  dryCoolerInletTemperatureC: number;
  dryCoolerOutletPressurePsi: number;
  pumpOutletPressurePsi: number;
  unavailableTelemetry: boolean;
  containerWidth: number;
}

const nominalArgs: SchematicStoryArgs = {
  tankALevelPercent: 82,
  tankBLevelPercent: 82,
  tankATemperatureC: 65.5,
  tankBTemperatureC: 65.5,
  tankAPressurePsi: 14.4,
  tankBPressurePsi: 14.4,
  fanSpeedPercent: 80,
  criticalFan: 10,
  criticalFanSpeedPercent: 12,
  flowRateGpm: 991,
  flowAlert: true,
  pumpSpeedHz: 30,
  dryCoolerInletTemperatureC: 65.5,
  dryCoolerOutletPressurePsi: 14.4,
  pumpOutletPressurePsi: 14.4,
  unavailableTelemetry: false,
  containerWidth: 1120,
};

const reading = (value: number, unit: string, unavailable: boolean) =>
  unavailable ? "—" : `${value.toLocaleString(undefined, { maximumFractionDigits: 1 })} ${unit}`;

function SchematicStory(args: SchematicStoryArgs) {
  const tankLevels = [args.tankALevelPercent, args.tankBLevelPercent];
  const tankTemperatures = [args.tankATemperatureC, args.tankBTemperatureC];
  const tankPressures = [args.tankAPressurePsi, args.tankBPressurePsi];
  const unavailable = args.unavailableTelemetry;
  const props: ContainerSystemSchematicProps = {
    tankBanks: (["A", "B"] as const).map((bank, bankIndex) => ({
      id: `Tank ${bank}`,
      tanks: Array.from({ length: 3 }, (_, tankIndex) => {
        const number = bankIndex * 3 + 3 - tankIndex;
        return {
          id: `tank-${number}`,
          label: `${number}`,
          levelPercent: unavailable ? null : tankLevels[bankIndex],
        };
      }),
      temperature: reading(tankTemperatures[bankIndex], "°C", unavailable),
      pressure: reading(tankPressures[bankIndex], "psi", unavailable),
    })) as ContainerSystemSchematicProps["tankBanks"],
    fans: Array.from({ length: 10 }, (_, index) => {
      const fanNumber = index + 1;
      const critical = fanNumber === args.criticalFan;
      return {
        id: `fan-${fanNumber}`,
        label: `${fanNumber}`,
        speedPercent: unavailable ? null : critical ? args.criticalFanSpeedPercent : args.fanSpeedPercent,
        status: critical && !unavailable ? ("critical" as const) : ("normal" as const),
      };
    }),
    flowRate: reading(args.flowRateGpm, "GPM", unavailable),
    flowAlert: args.flowAlert && !unavailable,
    pumpSpeed: reading(args.pumpSpeedHz, "Hz", unavailable),
    dryCoolerInletTemperature: reading(args.dryCoolerInletTemperatureC, "°C", unavailable),
    dryCoolerOutletPressure: reading(args.dryCoolerOutletPressurePsi, "psi", unavailable),
    pumpOutletPressure: reading(args.pumpOutletPressurePsi, "psi", unavailable),
  };

  return (
    <div className="min-h-screen bg-surface-base p-10 text-text-primary">
      <div style={{ width: args.containerWidth }}>
        <ContainerSystemSchematic {...props} />
      </div>
    </div>
  );
}

const percentControl = {
  control: { type: "range" as const, min: 0, max: 100, step: 1 },
};

const meta = {
  title: "Proto OS/Containers/Home/System schematic",
  component: SchematicStory,
  parameters: { layout: "fullscreen" },
  args: nominalArgs,
  argTypes: {
    tankALevelPercent: { ...percentControl, name: "Tank A level", table: { category: "Tanks" } },
    tankBLevelPercent: { ...percentControl, name: "Tank B level", table: { category: "Tanks" } },
    tankATemperatureC: {
      control: { type: "number", min: 0, max: 100, step: 0.1 },
      name: "Tank A temperature (°C)",
      table: { category: "Tanks" },
    },
    tankBTemperatureC: {
      control: { type: "number", min: 0, max: 100, step: 0.1 },
      name: "Tank B temperature (°C)",
      table: { category: "Tanks" },
    },
    tankAPressurePsi: {
      control: { type: "number", min: 0, max: 100, step: 0.1 },
      name: "Tank A pressure (psi)",
      table: { category: "Tanks" },
    },
    tankBPressurePsi: {
      control: { type: "number", min: 0, max: 100, step: 0.1 },
      name: "Tank B pressure (psi)",
      table: { category: "Tanks" },
    },
    fanSpeedPercent: { ...percentControl, name: "Normal fan speed", table: { category: "Dry cooler" } },
    criticalFan: {
      control: { type: "range", min: 0, max: 10, step: 1 },
      description: "Fan marked critical; use 0 for none.",
      table: { category: "Dry cooler" },
    },
    criticalFanSpeedPercent: {
      ...percentControl,
      name: "Critical fan speed",
      table: { category: "Dry cooler" },
    },
    flowRateGpm: {
      control: { type: "number", min: 0, step: 1 },
      name: "Flow rate (GPM)",
      table: { category: "Loop" },
    },
    flowAlert: { control: "boolean", table: { category: "Loop" } },
    pumpSpeedHz: {
      control: { type: "range", min: 0, max: 60, step: 1 },
      name: "Pump speed (Hz)",
      table: { category: "Loop" },
    },
    dryCoolerInletTemperatureC: {
      control: { type: "number", min: 0, max: 100, step: 0.1 },
      name: "Dry cooler inlet temperature (°C)",
      table: { category: "Loop" },
    },
    dryCoolerOutletPressurePsi: {
      control: { type: "number", min: 0, max: 100, step: 0.1 },
      name: "Dry cooler outlet pressure (psi)",
      table: { category: "Loop" },
    },
    pumpOutletPressurePsi: {
      control: { type: "number", min: 0, max: 100, step: 0.1 },
      name: "Pump outlet pressure (psi)",
      table: { category: "Loop" },
    },
    unavailableTelemetry: { control: "boolean", table: { category: "State" } },
    containerWidth: {
      control: { type: "range", min: 320, max: 1800, step: 40 },
      name: "Container width (px)",
      table: { category: "Layout" },
    },
  },
} satisfies Meta<typeof SchematicStory>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Playground: Story = {};
export const Nominal: Story = {};
export const Compact: Story = { args: { containerWidth: 840 } };
export const Phone: Story = { args: { containerWidth: 361 } };
export const Wide: Story = { args: { containerWidth: 1520 } };
export const Degraded: Story = {
  args: {
    tankBLevelPercent: 34,
    criticalFan: 3,
    criticalFanSpeedPercent: 0,
    flowAlert: true,
  },
};
export const UnavailableTelemetry: Story = { args: { unavailableTelemetry: true } };
