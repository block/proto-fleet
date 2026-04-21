import { useState } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import { StatusModal } from "./StatusModal";
import type { ComponentAddress, ComponentStatusData, ErrorData, MinerStatusData } from "./types";
import { variants } from "@/shared/components/Button";
import TemperatureValue from "@/shared/components/TemperatureValue";

const meta = {
  title: "Shared/StatusModal",
  component: StatusModal<ComponentAddress>,
  parameters: {
    layout: "centered",
  },
  decorators: [
    (Story) => (
      <div style={{ minWidth: "500px", minHeight: "400px" }}>
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof StatusModal<ComponentAddress>>;

export default meta;
type Story = StoryObj<typeof meta>;

// Mock data for miner status
const mockMinerStatusData: MinerStatusData = {
  props: {
    title: "Miner is operating normally",
    subtitle: "All systems running within expected parameters",
    errors: {
      hashboard: [],
      psu: [],
      fan: [],
      controlBoard: [],
      other: [],
    },
    isSleeping: false,
  },
  title: "Miner status",
  buttons: [
    {
      text: "Done",
      variant: variants.primary,
      onClick: () => {},
    },
  ],
  onDismiss: () => {},
};

// Mock data for miner with errors
const mockMinerStatusWithErrors: MinerStatusData = {
  props: {
    title: "Miner has 3 issues",
    subtitle: "Some components need attention",
    errors: {
      hashboard: [
        {
          componentName: "Hashboard 1",
          message: "Temperature exceeding threshold",
          timestamp: Date.now() - 3600000,
          onClick: () => {},
        },
      ],
      psu: [
        {
          componentName: "PSU 2",
          message: "Power efficiency below normal",
          timestamp: Date.now() - 7200000,
          onClick: () => {},
        },
      ],
      fan: [
        {
          componentName: "Fan 3",
          message: "RPM lower than expected",
          timestamp: Date.now() - 1800000,
          onClick: () => {},
        },
      ],
      controlBoard: [],
      other: [],
    },
    isSleeping: false,
  },
  title: "Miner status",
  buttons: [
    {
      text: "Done",
      variant: variants.primary,
      onClick: () => {},
    },
  ],
  onDismiss: () => {},
};

// Mock data for sleeping miner
const mockSleepingMinerStatus: MinerStatusData = {
  props: {
    title: "Miner is sleeping",
    subtitle: "Wake the miner to resume operations",
    errors: {
      hashboard: [],
      psu: [],
      fan: [],
      controlBoard: [],
      other: [],
    },
    isSleeping: true,
  },
  title: "Miner status",
  buttons: [
    {
      text: "Wake miner",
      variant: variants.secondary,
      onClick: () => {},
    },
    {
      text: "Done",
      variant: variants.primary,
      onClick: () => {},
    },
  ],
  onDismiss: () => {},
};

// Mock data for component status (Hashboard)
const mockHashboardStatus: ComponentStatusData = {
  props: {
    summary: "Hashboard 1 has multiple errors",
    componentType: "hashboard",
    errors: [
      {
        componentName: "Hashboard 1",
        message: "ASIC temperature is 95°C, exceeding safe threshold of 85°C",
        timestamp: Date.now() - 3600000,
      },
      {
        componentName: "Hashboard 1",
        message: "Hashrate dropped to 80 TH/s from expected 100 TH/s",
        timestamp: Date.now() - 1800000,
      },
    ],
    metrics: [
      {
        label: "Temperature",
        value: <TemperatureValue value={95} />,
      },
      {
        label: "Hashrate",
        value: "80 TH/s",
      },
      {
        label: "Efficiency",
        value: "25 J/TH",
      },
    ],
    metadata: {
      serialNumber: { label: "Serial Number", value: "HB-2024-001234" },
      model: { label: "Model", value: "S19 XP" },
      installedOn: { label: "Installed On", value: "06/15/24" },
      age: { label: "Age", value: "5 months" },
    },
  },
  title: "Hashboard status",
  buttons: [
    {
      text: "Done",
      variant: variants.primary,
      onClick: () => {},
    },
  ],
  onDismiss: () => {},
  onNavigateBack: () => {},
};

// Mock data for component status (Fan)
const mockFanStatus: ComponentStatusData = {
  props: {
    summary: "Fan 3 operating below optimal speed",
    componentType: "fan",
    errors: [
      {
        componentName: "Fan 3",
        message: "Fan speed is 2000 RPM, below minimum threshold of 3000 RPM",
        timestamp: Date.now() - 900000,
      },
    ],
    metrics: [
      {
        label: "100% PWM",
        value: "2000 RPM",
      },
    ],
    metadata: {
      serialNumber: { label: "Serial Number", value: "FAN-2024-003" },
      model: { label: "Model", value: "AFC1212DE" },
    },
  },
  title: "Fan status",
  buttons: [
    {
      text: "Done",
      variant: variants.primary,
      onClick: () => {},
    },
  ],
  onDismiss: () => {},
  onNavigateBack: () => {},
};

// Interactive wrapper component for stories
const InteractiveStatusModal = ({
  initialComponent,
  getMinerStatus,
  getComponentStatus,
  showBackButton = true,
}: {
  initialComponent?: ComponentAddress;
  getMinerStatus: () => MinerStatusData;
  getComponentStatus: (address: ComponentAddress) => ComponentStatusData;
  showBackButton?: boolean;
}) => {
  const [show, setShow] = useState(true);
  const [component, setComponent] = useState<ComponentAddress | undefined>(initialComponent);

  // Enhance getMinerStatus to add navigation
  const enhancedGetMinerStatus = (): MinerStatusData => {
    const data = getMinerStatus();

    // Add onClick handlers to errors to navigate to components
    const enhancedErrors = {
      hashboard: data.props.errors.hashboard.map((error, idx) => ({
        ...error,
        onClick: () =>
          setComponent({
            source: "HASHBOARD" as const,
            componentIndex: idx,
          }),
      })),
      psu: data.props.errors.psu.map((error, idx) => ({
        ...error,
        onClick: () =>
          setComponent({
            source: "PSU" as const,
            componentIndex: idx,
          }),
      })),
      fan: data.props.errors.fan.map((error, idx) => ({
        ...error,
        onClick: () =>
          setComponent({
            source: "FAN" as const,
            componentIndex: idx,
          }),
      })),
      controlBoard: data.props.errors.controlBoard.map((error, idx) => ({
        ...error,
        onClick: () =>
          setComponent({
            source: "SYSTEM" as const,
            componentIndex: idx,
          }),
      })),
      other: data.props.errors.other.map((error, idx) => ({
        ...error,
        onClick: () =>
          setComponent({
            source: "OTHER" as const,
            componentIndex: idx,
          }),
      })),
    };

    return {
      ...data,
      props: {
        ...data.props,
        errors: enhancedErrors,
      },
      onDismiss: () => setShow(false),
    };
  };

  // Enhance getComponentStatus to add dismiss and navigate handlers
  const enhancedGetComponentStatus = (address: ComponentAddress): ComponentStatusData | undefined => {
    const data = getComponentStatus(address);
    if (!data) return undefined;
    return {
      ...data,
      onDismiss: () => setShow(false),
      onNavigateBack: () => setComponent(undefined),
    };
  };

  return (
    <div>
      <button onClick={() => setShow(true)} style={{ marginBottom: "1rem" }}>
        Open Modal
      </button>

      <StatusModal
        componentAddress={component}
        getMinerStatus={enhancedGetMinerStatus}
        getComponentStatus={enhancedGetComponentStatus}
        open={show}
        showBackButton={showBackButton}
      />
    </div>
  );
};

// Stories

export const MinerStatusNormal: Story = {
  args: {
    componentAddress: undefined,
    getMinerStatus: () => mockMinerStatusData,
    getComponentStatus: () => mockHashboardStatus,
    open: true,
    showBackButton: true,
  },
  render: () => (
    <InteractiveStatusModal getMinerStatus={() => mockMinerStatusData} getComponentStatus={() => mockHashboardStatus} />
  ),
};

export const MinerStatusWithErrors: Story = {
  args: {
    componentAddress: undefined,
    getMinerStatus: () => mockMinerStatusWithErrors,
    getComponentStatus: (address: ComponentAddress) => {
      if (address.source === "FAN") {
        return mockFanStatus;
      }
      return mockHashboardStatus;
    },
    open: true,
    showBackButton: true,
  },
  render: () => (
    <InteractiveStatusModal
      getMinerStatus={() => mockMinerStatusWithErrors}
      getComponentStatus={(address) => {
        // Return different component data based on the address
        if (address.source === "FAN") {
          return mockFanStatus;
        }
        return mockHashboardStatus;
      }}
    />
  ),
};

export const MinerStatusSleeping: Story = {
  args: {
    componentAddress: undefined,
    getMinerStatus: () => mockSleepingMinerStatus,
    getComponentStatus: () => mockHashboardStatus,
    open: true,
    showBackButton: true,
  },
  render: () => (
    <InteractiveStatusModal
      getMinerStatus={() => mockSleepingMinerStatus}
      getComponentStatus={() => mockHashboardStatus}
    />
  ),
};

export const ComponentStatusHashboard: Story = {
  args: {
    componentAddress: { source: "HASHBOARD" as const, componentIndex: 0 },
    getMinerStatus: () => mockMinerStatusWithErrors,
    getComponentStatus: () => mockHashboardStatus,
    open: true,
    showBackButton: true,
  },
  render: () => (
    <InteractiveStatusModal
      initialComponent={{ source: "HASHBOARD" as const, componentIndex: 0 }}
      getMinerStatus={() => mockMinerStatusWithErrors}
      getComponentStatus={() => mockHashboardStatus}
    />
  ),
};

export const ComponentStatusFan: Story = {
  args: {
    componentAddress: { source: "FAN" as const, componentIndex: 2 },
    getMinerStatus: () => mockMinerStatusWithErrors,
    getComponentStatus: () => mockFanStatus,
    open: true,
    showBackButton: true,
  },
  render: () => (
    <InteractiveStatusModal
      initialComponent={{ source: "FAN" as const, componentIndex: 2 }}
      getMinerStatus={() => mockMinerStatusWithErrors}
      getComponentStatus={() => mockFanStatus}
    />
  ),
};

export const ComponentStatusNoBackButton: Story = {
  args: {
    componentAddress: { source: "HASHBOARD" as const, componentIndex: 0 },
    getMinerStatus: () => mockMinerStatusWithErrors,
    getComponentStatus: () => mockHashboardStatus,
    open: true,
    showBackButton: false,
  },
  render: () => (
    <InteractiveStatusModal
      initialComponent={{ source: "HASHBOARD" as const, componentIndex: 0 }}
      getMinerStatus={() => mockMinerStatusWithErrors}
      getComponentStatus={() => mockHashboardStatus}
      showBackButton={false}
    />
  ),
};

// Create a proper React component for the Playground story
const PlaygroundComponent = (args: any) => {
  const [component, setComponent] = useState<ComponentAddress | undefined>(
    args.componentAddress as ComponentAddress | undefined,
  );
  const [show, setShow] = useState(args.open);

  // Enhance getMinerStatus to add navigation
  const enhancedGetMinerStatus = (): MinerStatusData => {
    const data = args.getMinerStatus();

    // Add onClick handlers to errors to navigate to components
    const enhancedErrors = {
      hashboard: data.props.errors.hashboard.map((error: ErrorData, idx: number) => ({
        ...error,
        onClick: () =>
          setComponent({
            source: "HASHBOARD" as const,
            componentIndex: idx,
          }),
      })),
      psu: data.props.errors.psu.map((error: ErrorData, idx: number) => ({
        ...error,
        onClick: () =>
          setComponent({
            source: "PSU" as const,
            componentIndex: idx,
          }),
      })),
      fan: data.props.errors.fan.map((error: ErrorData, idx: number) => ({
        ...error,
        onClick: () =>
          setComponent({
            source: "FAN" as const,
            componentIndex: idx,
          }),
      })),
      controlBoard: data.props.errors.controlBoard,
    };

    return {
      ...data,
      props: {
        ...data.props,
        errors: enhancedErrors,
      },
      buttons: data.buttons.map((btn: any) => ({
        ...btn,
        onClick: btn.text === "Done" ? () => setShow(false) : btn.onClick,
      })),
      onDismiss: () => setShow(false),
    };
  };

  // Enhance getComponentStatus to add navigate handler
  const enhancedGetComponentStatus = (address: ComponentAddress): ComponentStatusData | undefined => {
    const data = args.getComponentStatus(address);
    if (!data) return undefined;
    return {
      ...data,
      onDismiss: () => setShow(false),
      onNavigateBack: () => setComponent(undefined),
    };
  };

  return (
    <div>
      <div style={{ marginBottom: "2rem" }}>
        <button onClick={() => setShow(true)}>Open Modal</button>
        <button onClick={() => setComponent(undefined)} style={{ marginLeft: "1rem" }}>
          Reset to Miner Status
        </button>
        <button
          onClick={() => setComponent({ source: "HASHBOARD" as const, componentIndex: 0 })}
          style={{ marginLeft: "1rem" }}
        >
          Go to Hashboard
        </button>
      </div>

      <StatusModal
        componentAddress={component}
        getMinerStatus={enhancedGetMinerStatus}
        getComponentStatus={enhancedGetComponentStatus}
        open={show}
        showBackButton={args.showBackButton}
      />
    </div>
  );
};

// Playground story for testing different configurations
export const Playground: Story = {
  args: {
    componentAddress: undefined,
    getMinerStatus: () => mockMinerStatusWithErrors,
    getComponentStatus: () => mockHashboardStatus,
    open: true,
    showBackButton: true,
  },
  render: PlaygroundComponent,
};
