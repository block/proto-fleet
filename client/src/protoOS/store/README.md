# ProtoOS Zustand Store Overview

This document provides a comprehensive overview of the unified Zustand store architecture in the ProtoOS application.

## Store Architecture

The ProtoOS store uses a **slice-based architecture** with a single unified store (`useMinerStore`) that combines multiple slices:

```
/protoOS/store/
├── useMinerStore.ts              # Main unified store
├── index.ts                      # Clean public API exports
├── types.ts                      # TypeScript interfaces and types
├── slices/
│   ├── hardwareSlice.ts          # Hardware state (miner, hashboards, ASICs, PSUs, fans, control board)
│   ├── telemetrySlice.ts         # Real-time telemetry data
│   └── uiSlice.ts               # UI state (duration, etc.)
├── hooks/
│   ├── useHardware.ts           # Hardware slice access hooks
│   ├── useTelemetry.ts          # Telemetry slice access hooks
│   ├── useUI.ts                 # UI slice access hooks
│   └── useMiner.ts              # Combined hardware + telemetry hooks
└── utils/
    ├── telemetryUtils.ts        # Telemetry utility functions
    ├── getAsicId.ts            # ASIC ID generation utility
    └── getAsicName.ts          # ASIC name generation utility
```

## Main Store Interface

```typescript
interface MinerStore {
  hardware: HardwareSlice; // Static miner hardware information
  telemetry: TelemetrySlice; // Real-time telemetry data
  ui: UISlice; // UI state management
}
```

## Hardware Slice

The hardware slice stores structural information about the miner and its components:

### State

- **miner**: `MinerHardwareData | null` - Top-level miner info
- **controlBoard**: `ControlBoardHardwareData | null` - Control board info (serial, firmware, CPU/MPU details)
- **hashboards**: `Map<string, HashboardHardwareData>` - Hashboards keyed by serial number
- **asics**: `Map<string, AsicHardwareData>` - ASICs keyed by unique ID
- **psus**: `Map<number, PsuHardwareData>` - PSUs keyed by slot number (1-3)
- **fans**: `Map<number, FanHardwareData>` - Fans keyed by ID

### Hardware Data Types

```typescript
// Control Board
export interface ControlBoardHardwareData {
  serial?: string;
  boardId?: string;
  machineName?: string;
  firmware?: {
    name?: string;
    version?: string;
    variant?: string;
    gitHash?: string;
    imageHash?: string;
  };
  mpu?: {
    /* CPU/MPU details */
  };
}

// Hashboard
export interface HashboardHardwareData {
  serial: string;
  slot?: number;
  bay?: number;
  board?: string;
  slotIndexByBay?: number;
  asicIds?: string[];
  // Additional fields from API
  apiVersion?: string;
  chipId?: string;
  port?: number;
  miningAsic?: "BZM" | "MC1" | "MC2";
  miningAsicCount?: number;
  temperatureSensorCount?: number;
  ecLogsPath?: string;
  firmware?: {
    /* firmware details */
  };
  bootloader?: {
    /* bootloader details */
  };
}

// PSU
export interface PsuHardwareData {
  id: number; // Slot number (1-3)
  serial?: string;
  slot?: number;
  manufacturer?: string;
  model?: string;
  hwRevision?: string;
  firmware?: {
    appVersion?: string;
    bootloaderVersion?: string;
  };
}

// Fan
export interface FanHardwareData {
  id: number; // Fan identifier
  name?: string;
}
```

## Telemetry Slice

The telemetry slice stores real-time measurement data:

### State

- **miner**: `MinerTelemetryData | null` - Miner-level metrics
- **hashboards**: `Map<string, HashboardTelemetryData>` - Hashboard telemetry keyed by serial
- **asics**: `Map<string, AsicTelemetryData>` - ASIC telemetry keyed by ID
- **psus**: `Map<number, PsuTelemetryData>` - PSU telemetry keyed by slot number
- **fans**: `Map<number, FanTelemetryData>` - Fan telemetry keyed by ID
- **lastApiResponse**: `any | null` - Last API response
- **lastUpdated**: `number` - Last update timestamp
- **intervalMs**: `number` - Sampling interval

### Telemetry Data Types

```typescript
// PSU Telemetry
export interface PsuTelemetryData {
  id: number;
  inputVoltage?: MetricTelemetry;
  inputCurrent?: MetricTelemetry;
  inputPower?: MetricTelemetry;
  outputVoltage?: MetricTelemetry;
  outputCurrent?: MetricTelemetry;
  outputPower?: MetricTelemetry;
  temperatures?: MetricTelemetry[];
}

// Fan Telemetry
export interface FanTelemetryData {
  id: number;
  rpm?: MetricTelemetry;
  percentage?: MetricTelemetry;
  minRpm?: MetricTelemetry;
  maxRpm?: MetricTelemetry;
}
```

## Key Data Types

### Measurement Type

```typescript
export type Measurement = {
  value: number | null;
  units: MetricUnit | undefined;
  formatted?: string;
};
```

The `Measurement` type represents a single data point with value, units, and optional formatted display string. This is used throughout the store for temperature readings, power measurements, hashrate values, etc.

### MetricTelemetry

```typescript
export type MetricTelemetry = {
  timeSeries?: MetricTimeSeries;
  latest?: Measurement;
};
```

Combines both historical time series data and the latest measurement for a given metric.

### MetricTimeSeries

```typescript
export interface MetricTimeSeries {
  aggregates?: {
    min?: Measurement;
    avg?: Measurement;
    max?: Measurement;
  };
  units: MetricUnit;
  values: (number | null)[];
  startTime: number;
  endTime: number;
}
```

Used for time-series data like hashrate, temperature, power, and efficiency over time.

### Supported Units

```typescript
export type MetricUnit =
  | TemperatureUnit // "C" | "F"
  | PowerUnit // "W" | "kW" | "MW"
  | HashrateUnit // "TH/s" | "GH/s" | "MH/s"
  | EfficiencyUnit // "J/TH"
  | PercentageUnit // "%"
  | RpmUnit // "RPM"
  | VoltageUnit // "V" | "mV"
  | CurrentUnit; // "A" | "mA"
```

## API Integration

### Data Population Pattern

API hooks follow a clean separation pattern:

1. **Fetch data** - API hook fetches data and sets local state
2. **useEffect watches** - Separate `useEffect` listens for data changes
3. **Update store** - useEffect updates the appropriate store slice(s)

Example from `useHardware`:

```typescript
// Fetch and set local state
useEffect(() => {
  api.getHardware().then((res) => {
    setHashboardsInfo(res.hashboards);
    setPsusInfo(res.psus);
    setFansInfo(res.fans);
    setControlBoardInfo(res.controlBoard);
  });
}, [api]);

// Separate useEffect updates store when data changes
useEffect(() => {
  if (!hashboardsInfo) return;
  hashboardsInfo.forEach((hb) => {
    useMinerStore.getState().hardware.addHashboard(hb);
  });
}, [hashboardsInfo]);

// Similar pattern for psusInfo, fansInfo, controlBoardInfo
```

### Store Update Actions

**Hardware Slice Actions:**

- `setMiner()`, `getMiner()`
- `setControlBoard()`, `getControlBoard()`
- `addHashboard()`, `getHashboard()`, `getHashboardsByBay()`
- `addAsic()`, `getAsic()`, `getAsicsByHashboard()`
- `addPsu()`, `getPsu()`, `getAllPsus()`
- `addFan()`, `getFan()`, `getAllFans()`

**Telemetry Slice Actions:**

- `updateTimeSeriesTelemetry()` - Updates from time series API
- `updateLatestTelemetry()` - Updates from real-time API
- `updatePsuTelemetry()` - Updates PSU metrics
- `updateFanTelemetry()` - Updates fan metrics
- `updateHashboardTemperatures()` - Updates inlet/outlet temps

## Usage Patterns

### Direct Store Access

```typescript
import { useMinerStore } from "@/protoOS/store";

// Get full store state
const store = useMinerStore();

// Get specific slice
const hardware = useMinerStore((state) => state.hardware);
const telemetry = useMinerStore((state) => state.telemetry);

// Get specific data with useShallow for better performance
const fans = useMinerStore(useShallow((state) => Array.from(state.telemetry.fans.values())));

// Call slice actions
useMinerStore.getState().hardware.addHashboard(hashboard);
useMinerStore.getState().telemetry.updateFanTelemetry(fanId, data);
```

### Convenience Hooks (Recommended)

```typescript
import {
  useMinerHashboards,
  useBayCount,
  useFansTelemetry,
  usePsusTelemetry,
  useDuration,
  useChartDataForMetric,
} from "@/protoOS/store";

// Get integrated data (hardware + telemetry combined)
const hashboards = useMinerHashboards(); // HashboardData[]

// Get hardware info
const bayCount = useBayCount();

// Get telemetry data
const fans = useFansTelemetry(); // FanTelemetryData[]
const psus = usePsusTelemetry(); // PsuTelemetryData[]

// Get UI state
const duration = useDuration();

// Get chart-ready data for KPI components
const chartData = useChartDataForMetric("hashrate");
```

### Telemetry Utility Functions

```typescript
import { convertValueUnits, formatValue, convertAndFormatMeasurement } from "@/protoOS/store";

// Convert units while preserving type
const converted = convertValueUnits(measurement, "F");

// Format measurement for display
const formatted = formatValue(measurement, true);

// Convert and format in one step
const formattedPower = convertAndFormatMeasurement(measurement, "kW", true);
```

## Component Integration Examples

### Reading Fan Data from Store

```typescript
import { useFansTelemetry, useBayCount } from "@/protoOS/store";
import { useCoolingStatus } from "@/protoOS/api";

const Temperature = () => {
  // Use convenience hooks to get data from store
  const bayCount = useBayCount();
  const fans = useFansTelemetry();

  // Fetch cooling status to populate store (hook handles store updates)
  useCoolingStatus({ poll: true });

  return (
    <div>
      {fans.map(fan => (
        <div key={fan.id}>
          RPM: {fan.rpm?.latest?.value}
          Speed: {fan.percentage?.latest?.value}%
        </div>
      ))}
    </div>
  );
};
```

## Key Benefits

1. **Single Source of Truth**: One unified store instead of multiple separate stores
2. **Better Performance**: Slice-based architecture enables more granular subscriptions
3. **Type Safety**: Full TypeScript support with proper slice interfaces and Measurement types
4. **Clean API**: Convenience hooks provide clean, focused interfaces for common use cases
5. **Maintainable**: Clear separation of concerns between slices
6. **Chart Ready**: Built-in chart data transformation for KPI components
7. **Unit Conversion**: Intelligent unit conversion system with type safety
8. **Formatted Display**: Automatic value formatting with proper unit display
9. **Comprehensive Hardware Tracking**: Tracks all miner components (hashboards, ASICs, PSUs, fans, control board)
10. **Flexible Telemetry**: Supports both time-series and latest values for all metrics

## Recent Architectural Improvements

### Hardware Expansion

- Added **PSU support** (hardware info + telemetry for voltage, current, power, temperatures)
- Added **Fan support** (hardware info + telemetry for RPM, speed percentage, min/max RPM)
- Added **Control Board support** (serial, firmware, CPU/MPU details)
- Enhanced **Hashboard data** with additional API fields (firmware, bootloader, chip ID, etc.)

### Data Separation and Clean Architecture

**Hardware vs Telemetry Data Separation**

- **Hardware Slice**: Stores structural data (IDs, positions, relationships, firmware versions)
- **Telemetry Slice**: Stores only time-series measurement data
- Clean separation prevents data duplication and confusion

**API Hook Pattern**

- Hooks fetch data and set local state
- Separate `useEffect` blocks watch for changes and update store
- Pattern used in: `useHardware`, `useCoolingStatus`, `useHashboardStatus`
- Better separation of concerns and easier testing

### Type System Enhancements

- Added unit types: `RpmUnit`, `VoltageUnit`, `CurrentUnit`
- Expanded `MetricUnit` union to support all hardware metrics
- Comprehensive type definitions for all hardware components
- Proper generic types for combined data (`PsuData`, `FanData`)

## Migration Notes

- `CurrentValue` type has been renamed to `Measurement` for better semantic clarity
- All measurements now use `Measurement` objects with proper unit types
- New hardware components (PSUs, fans, control board) are available in the store
- Use `useShallow` from zustand for better performance when reading arrays/objects from store
- Fan min/max RPM stored in telemetry slice (not hardware) as they're metric constraints
