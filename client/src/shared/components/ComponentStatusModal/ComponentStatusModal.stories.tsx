import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";
import ComponentStatusModalComponent from "./ComponentStatusModal";
import type { ComponentError } from "./types";

// Sample error data for different scenarios
const sampleFanErrors: ComponentError[] = [
  {
    id: "fan-1-1",
    componentType: "fan",
    componentName: "Fan 2",
    title: "Damaged connector or missing power",
    message: "Damaged connector or missing power",
    timestamp: 1723654440,
    details: JSON.stringify(
      {
        FanSlow: {
          fan_rpm_target: 6000,
          fan_rpm_tach: 900,
        },
      },
      null,
      2,
    ),
    notificationError: {
      component_index: 1,
      error_code: "FanSlow",
      error_level: "Error",
      message: "Damaged connector or missing power",
      inserted_at: 1723654440,
    },
  },
  {
    id: "fan-2-2",
    componentType: "fan",
    componentName: "Fan 3",
    title: "Damaged connector or missing power",
    message: "Damaged connector or missing power",
    timestamp: 1723654440,
    notificationError: {
      component_index: 2,
      error_code: "FanSlow",
      error_level: "Error",
      message: "Damaged connector or missing power",
      inserted_at: 1723654440,
    },
  },
];

const sampleHashboardErrors: ComponentError[] = [
  {
    id: "hashboard-1-1",
    componentType: "hashboard",
    componentName: "Hashboard 4",
    title: "Insufficient cooling",
    message: "Insufficient cooling",
    timestamp: 1723654440,
    details: JSON.stringify(
      {
        TemperatureHigh: {
          max_temp_c: 95,
          threshold_c: 85,
        },
      },
      null,
      2,
    ),
    notificationError: {
      hashboard_index: 3,
      error_code: "TemperatureHigh",
      error_level: "Warning",
      message: "Insufficient cooling",
      inserted_at: 1723654440,
    },
  },
];

const multipleMixedErrors: ComponentError[] = [
  ...sampleFanErrors,
  ...sampleHashboardErrors,
  {
    id: "psu-1-1",
    componentType: "psu",
    componentName: "PSU 1",
    title: "PSU Over Temperature",
    message: "PSU temperature exceeds safe operating limits",
    timestamp: 1723654500,
    notificationError: {
      error_code: "PsuOverTemp",
      error_level: "Error",
      message: "PSU temperature exceeds safe operating limits",
      inserted_at: 1723654500,
    },
  },
  {
    id: "controlBoard-1-1",
    componentType: "controlBoard",
    componentName: "Control Board",
    title: "High CPU Usage",
    message: "CPU usage is above 90%",
    timestamp: 1723654600,
    notificationError: {
      error_code: "HighCpuUsage",
      error_level: "Warning",
      message: "CPU usage is above 90%",
      inserted_at: 1723654600,
    },
  },
];

const testErrors = {
  fanErrors: sampleFanErrors,
  hashboardError: sampleHashboardErrors,
  multipleMixed: multipleMixedErrors,
  singleError: [sampleFanErrors[0]],
};

type TestErrorType = keyof typeof testErrors;

export const ComponentStatusModal = ({
  testErrorType,
}: {
  testErrorType: TestErrorType;
}) => {
  return (
    <div>
      <ComponentStatusModalComponent
        errors={testErrors[testErrorType]}
        onDismiss={() => {
          // Modal dismissed
        }}
      />
    </div>
  );
};

ComponentStatusModal.args = {
  testErrorType: "multipleMixed",
};

ComponentStatusModal.argTypes = {
  testErrorType: {
    control: "select",
    options: Object.keys(testErrors),
    description: "Select error scenario to display",
  },
};

export default {
  title: "ProtoOS/Diagnostic/ComponentStatusModal",
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
