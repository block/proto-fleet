import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";
import ComponentStatusModalComponent from "./ComponentStatusModal";
import type { ComponentError } from "./types";

// ============= Hashboard Stories =============

const hashboardErrors: ComponentError[] = [
  {
    id: "hb-error-1",
    componentType: "hashboard",
    componentName: "Hashboard 3",
    title: "Insufficient cooling",
    message: "Insufficient cooling",
    timestamp: 1730000000,
    severity: "warning",
    details: "{}",
    notificationError: {},
  },
  {
    id: "hb-error-2",
    componentType: "hashboard",
    componentName: "Hashboard 3",
    title: "Temperature threshold exceeded",
    message: "Temperature threshold exceeded",
    timestamp: 1730001000,
    severity: "error",
    details: "{}",
    notificationError: {},
  },
  {
    id: "hb-error-3",
    componentType: "hashboard",
    componentName: "Hashboard 3",
    title: "Insufficient cooling",
    message: "Insufficient cooling",
    timestamp: 1730000000,
    severity: "warning",
    details: "{}",
    notificationError: {},
  },
  {
    id: "hb-error-4",
    componentType: "hashboard",
    componentName: "Hashboard 3",
    title: "Temperature threshold exceeded",
    message: "Temperature threshold exceeded",
    timestamp: 1730001000,
    severity: "error",
    details: "{}",
    notificationError: {},
  },
];

export const HashboardWithErrors = () => (
  <ComponentStatusModalComponent
    summary="Hashboard 3 has multiple issues"
    componentType="hashboard"
    issues={hashboardErrors}
    metrics={[
      { label: "Chip Avg", value: "27.9" },
      { label: "Chip High", value: "29.9" },
    ]}
    metadata={{
      serialNumber: "PM-H132435034",
      model: "Model 1234",
      installedOn: "8/14/25",
      age: "1 year, 28 days old",
    }}
    onDismiss={() => {}}
    navigateBack={() => {}}
  />
);

export const HashboardNoErrors = () => (
  <ComponentStatusModalComponent
    summary="Hashboard 3 operating normally"
    componentType="hashboard"
    issues={[]}
    metrics={[
      { label: "Chip Avg", value: "27.9" },
      { label: "Chip High", value: "29.9" },
    ]}
    metadata={{
      serialNumber: "PM-H132435034",
      model: "Model 1234",
      installedOn: "8/14/25",
      age: "1 year, 28 days old",
    }}
    onDismiss={() => {}}
  />
);

// ============= Fan Stories =============

const fanErrors: ComponentError[] = [
  {
    id: "fan-error-1",
    componentType: "fan",
    componentName: "Fan 2",
    title: "Damaged connector or missing power",
    message: "Damaged connector or missing power",
    timestamp: 1730000000,
    severity: "error",
    details: "{}",
    notificationError: {},
  },
  {
    id: "fan-error-2",
    componentType: "fan",
    componentName: "Fan 3",
    title: "Fan speed below threshold",
    message: "Fan speed below threshold",
    timestamp: 1730001000,
    severity: "warning",
    details: "{}",
    notificationError: {},
  },
];

export const FanWithErrors = () => (
  <ComponentStatusModalComponent
    summary="Fan unit has multiple issues"
    componentType="fan"
    issues={fanErrors}
    metrics={[
      { label: "Speed", value: "5,850 RPM" },
      { label: "PWM", value: "75%" },
    ]}
    metadata={{
      serialNumber: "PM-FAN-987654",
      model: "Model F-200",
      installedOn: "7/10/25",
      age: "2 months, 15 days old",
    }}
    onDismiss={() => {}}
    navigateBack={() => {}}
  />
);

export const FanNoErrors = () => (
  <ComponentStatusModalComponent
    summary="Fan unit operating normally"
    componentType="fan"
    issues={[]}
    metrics={[
      { label: "Speed", value: "5,850 RPM" },
      { label: "PWM", value: "75%" },
    ]}
    metadata={{
      serialNumber: "PM-FAN-987654",
      model: "Model F-200",
      installedOn: "7/10/25",
      age: "2 months, 15 days old",
    }}
    onDismiss={() => {}}
  />
);

// ============= PSU Stories =============

const psuErrors: ComponentError[] = [
  {
    id: "psu-error-1",
    componentType: "psu",
    componentName: "PSU 1",
    title: "PSU temperature exceeds safe operating limits",
    message: "PSU temperature exceeds safe operating limits",
    timestamp: 1730000000,
    severity: "critical",
    details: "{}",
    notificationError: {},
  },
  {
    id: "psu-error-2",
    componentType: "psu",
    componentName: "PSU 1",
    title: "Output voltage unstable",
    message: "Output voltage fluctuating outside normal range",
    timestamp: 1730001000,
    severity: "warning",
    details: "{}",
    notificationError: {},
  },
];

export const PSUWithErrors = () => (
  <ComponentStatusModalComponent
    summary="PSU 1 has multiple issues"
    componentType="psu"
    issues={psuErrors}
    metrics={[
      { label: "Current power", value: "1.3 kW" },
      { label: "Input power", value: "1.3 kW" },
      { label: "Output power", value: "1.2 kW" },
      { label: "Temperature", value: "58° C" },
      { label: "Output voltage", value: "12 V" },
      { label: "Output current", value: "100.0 A" },
    ]}
    metadata={{
      serialNumber: "PM-PSU-456789",
      model: "Model PSU-3000",
      installedOn: "6/1/25",
      age: "3 months, 24 days old",
    }}
    onDismiss={() => {}}
    navigateBack={() => {}}
  />
);

export const PSUNoErrors = () => (
  <ComponentStatusModalComponent
    summary="PSU 1 operating normally"
    componentType="psu"
    issues={[]}
    metrics={[
      { label: "Current power", value: "1.3 kW" },
      { label: "Input power", value: "1.3 kW" },
      { label: "Output power", value: "1.2 kW" },
      { label: "Temperature", value: "58° C" },
      { label: "Output voltage", value: "12 V" },
      { label: "Output current", value: "100.0 A" },
    ]}
    metadata={{
      serialNumber: "PM-PSU-456789",
      model: "Model PSU-3000",
      installedOn: "6/1/25",
      age: "3 months, 24 days old",
    }}
    onDismiss={() => {}}
  />
);

// ============= Control Board Stories =============

const controlBoardErrors: ComponentError[] = [
  {
    id: "cb-error-1",
    componentType: "controlBoard",
    componentName: "Control Board",
    title: "CPU usage is above 90%",
    message: "CPU usage is above 90%",
    timestamp: 1730000000,
    severity: "warning",
    details: "{}",
    notificationError: {},
  },
  {
    id: "cb-error-2",
    componentType: "controlBoard",
    componentName: "Control Board",
    title: "Memory usage exceeds 85%",
    message: "Memory usage exceeds 85%",
    timestamp: 1730001000,
    severity: "warning",
    details: "{}",
    notificationError: {},
  },
];

export const ControlBoardWithErrors = () => (
  <ComponentStatusModalComponent
    summary="Control board has performance warnings"
    componentType="controlBoard"
    issues={controlBoardErrors}
    metrics={[
      { label: "CPU capacity", value: "92%" },
      { label: "Latency", value: "12ms" },
    ]}
    metadata={{
      serialNumber: "PM-CB-112233",
      model: "Model CB-X1",
      installedOn: "5/15/25",
      age: "4 months, 10 days old",
    }}
    onDismiss={() => {}}
  />
);

export const ControlBoardNoErrors = () => (
  <ComponentStatusModalComponent
    summary="Control board operating normally"
    componentType="controlBoard"
    issues={[]}
    metrics={[
      { label: "CPU capacity", value: "92%" },
      { label: "Latency", value: "12ms" },
    ]}
    metadata={{
      serialNumber: "PM-CB-112233",
      model: "Model CB-X1",
      installedOn: "5/15/25",
      age: "4 months, 10 days old",
    }}
    onDismiss={() => {}}
  />
);

// ============= Special Cases =============

export const HashboardErrorsOnly = () => (
  <ComponentStatusModalComponent
    summary="Hashboard 3 has multiple issues"
    componentType="hashboard"
    issues={hashboardErrors}
    onDismiss={() => {}}
  />
);

export const HashboardDetailsOnly = () => (
  <ComponentStatusModalComponent
    summary="Hashboard 3 status"
    componentType="hashboard"
    issues={[]}
    metrics={[
      { label: "CPU capacity", value: "92%" },
      { label: "Latency", value: "12ms" },
    ]}
    metadata={{
      serialNumber: "PM-CB-112233",
      model: "Model CB-X1",
      installedOn: "5/15/25",
      age: "4 months, 10 days old",
    }}
    onDismiss={() => {}}
  />
);

export default {
  title: "Shared/ComponentStatusModal",
  component: ComponentStatusModalComponent,
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
