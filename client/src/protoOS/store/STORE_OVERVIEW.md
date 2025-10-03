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
│   ├── hardwareSlice.ts          # Hardware state (miner, hashboards, ASICs)
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

### MetricTimeSeries

```typescript
export interface MetricTimeSeries {
  aggregates: { min: number; avg: number; max: number };
  units: MetricUnit;
  values: (number | null)[];
  startTime: number;
  endTime: number;
}
```

Used for time-series data like hashrate, temperature, power, and efficiency over time.

### Enhanced Telemetry Data

```typescript
export interface HashboardTelemetryData extends Telemetry {
  serial: string;
  inletTemp?: Measurement;
  outletTemp?: Measurement;
  avgAsicTemp?: Measurement;
  maxAsicTemp?: Measurement;
}

export interface AsicTelemetryData extends Telemetry {
  id: string;
}
```

**Recent Changes:**

- Removed redundant `id` field from `HashboardTelemetryData` (serial serves as the identifier)
- Simplified `AsicTelemetryData` to only contain telemetry data
- Positional data (`index`, `hashboardIndex`) moved to hardware slice where it belongs

## Usage Patterns

### Direct Store Access

```typescript
import { useMinerStore } from "@/protoOS/store";

// Get full store state
const store = useMinerStore();

// Get specific slice
const hardware = useMinerStore((state) => state.hardware);
const telemetry = useMinerStore((state) => state.telemetry);

// Call slice actions
useMinerStore.getState().hardware.addHashboard(hashboard);
useMinerStore.getState().telemetry.updateTelemetryData(data);
```

### Convenience Hooks (Recommended)

```typescript
import {
  useMinerHashboards,
  useDuration,
  useChartDataForMetric,
} from "@/protoOS/store";

// Get integrated data (hardware + telemetry combined)
const hashboards = useMinerHashboards(); // HashboardData[]

// Get UI state
const duration = useDuration();

// Get chart-ready data for KPI components
const chartData = useChartDataForMetric("hashrate");
```

### Telemetry Utility Functions

```typescript
import {
  getCurrentValue,
  convertValueUnits,
  formatValue,
  convertAndFormatMeasurement,
} from "@/protoOS/store";

// Get current value from time series with unit conversion
const currentTemp = getCurrentValue(
  metric.temperature,
  "F", // preferred units
  true, // display units
); // Returns Measurement with formatted string

// Convert and format in one step
const formattedPower = convertAndFormatMeasurement(measurement, "kW", true); // Returns formatted string directly

// Convert units while preserving type
const converted = convertValueUnits(measurement, "F");

// Format measurement for display
const formatted = formatValue(measurement, true);
```

### Slice-Specific Hooks

```typescript
import {
  useHashboardHardware,
  useMinerTelemetry,
  useSetDuration,
  useUpdateTelemetryData,
} from "@/protoOS/store";

// Hardware data
const hashboardHardware = useHashboardHardware();

// Telemetry data and actions
const minerTelemetry = useMinerTelemetry();
const updateTelemetry = useUpdateTelemetryData();

// UI actions
const setDuration = useSetDuration();
```

## Chart Integration

### KPI Line Chart Integration

The store provides seamless integration with KPI charts through the `useChartDataForMetric` hook:

```typescript
import { useChartDataForMetric } from "@/protoOS/store";
import { ChartData } from "@/shared/components/LineChart";

// Get chart-ready data for any metric
const chartData: ChartData[] = useChartDataForMetric("hashrate");
// Returns: [{ datetime: number, miner: number, HB001: number, HB002: number, ... }]
```

This hook automatically:

- Combines miner and hashboard telemetry data
- Transforms it into chart-compatible format
- Handles proper datetime alignment
- Includes all hashboard series for the selected metric

## Utility Functions

### Telemetry Processing

- `getAsicId(hashboardSerial, asicIndex)` - Get standardized ASIC identifiers
- `getAsicName(totalAsics, asicIndex)` - Generate ASIC display names (A0, A1, B0, B1, etc.)

### Unit Conversion & Formatting

- `getCurrentValue(metric, preferredUnits, displayUnits)` - Extract current value with conversion
- `convertValueUnits(measurement, preferredUnits)` - Convert between compatible units
- `formatValue(measurement, displayUnits)` - Format measurement for display
- `convertAndFormatMeasurement(measurement, preferredUnits, displayUnits)` - Convert and format in one step

## Key Benefits

1. **Single Source of Truth**: One unified store instead of multiple separate stores
2. **Better Performance**: Slice-based architecture enables more granular subscriptions
3. **Type Safety**: Full TypeScript support with proper slice interfaces and Measurement types
4. **Clean API**: Convenience hooks provide clean, focused interfaces for common use cases
5. **Maintainable**: Clear separation of concerns between slices
6. **Chart Ready**: Built-in chart data transformation for KPI components
7. **Unit Conversion**: Intelligent unit conversion system with type safety
8. **Formatted Display**: Automatic value formatting with proper unit display

## Recent Architectural Improvements

### Data Separation and Clean Architecture

**Hardware vs Telemetry Data Separation**

- **Hardware Slice**: Stores structural data (IDs, positions, relationships)
  - ASIC `index` and `hashboardIndex` now live in hardware slice
  - Hardware data populated by `useHardware()` and `useHashboardStatus()`
- **Telemetry Slice**: Stores only time-series measurement data
  - No longer stores positional/structural information
  - Focused purely on metrics and measurements

**ASIC Naming System**

- Removed `name` storage from store interfaces
- Added `getAsicName(totalAsics, asicIndex)` utility for dynamic name generation
- Names computed on-demand: A0, A1, B0, B1, etc.
- Cleaner architecture without redundant stored data

**Enhanced API Integration**

- `useTimeSeries` now updates both hardware and telemetry slices appropriately
- Hardware slice receives positional data from time series API
- Telemetry slice receives measurement data
- Better data flow and single source of truth

### Hook Improvements

**Simplified Data Access**

```typescript
// All hooks now use spread pattern for cleaner code
const asicData = useMinerAsic(asicId); // Contains both hardware + telemetry
// No need to manually list every property - spreads both objects
```

**Better Performance**

- Removed unnecessary type predicate filters
- More efficient array operations (reduce vs filter+map)
- Optimized dependency arrays to prevent infinite loops

## Migration Notes

- `CurrentValue` type has been renamed to `Measurement` for better semantic clarity
- All temperature measurements now use `Measurement` objects instead of raw values
- Chart data hook moved from features to store for better organization
- New utility functions provide streamlined value conversion and formatting
- Enhanced telemetry types include direct `Measurement` objects for common temperature readings
- **Breaking**: `useHashboardStats` renamed to `useHashboardStatus` for better clarity
- **Breaking**: ASIC positional data moved from telemetry to hardware slice
