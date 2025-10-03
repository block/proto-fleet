# Store Refactor TODOs

This document lists all remaining TODOs related to the store refactor

- **Generic Metric Processing** (`telemetrySlice.ts:168`)
  - Make metric processing more generic to avoid updating hardcoded list for each new metric
  - Consider dynamic metric discovery from API response

- **Unit Types from Generated API** (`types.ts:5`)
  - Currently unit types are manually defined, should come from generated API types
  - Waiting on backend to provide proper unit enums in OpenAPI spec

- **Control Board Serial from API** (`telemetrySlice.ts:157`)
  - Currently hardcoded as "MAIN_001"
  - Need API endpoint to provide actual control board serial number

- **ASIC Index Always Provided** (`telemetrySlice.ts:300`)
  - MDK-API.json needs update to always provide ASIC index
  - Currently causes issues when index is undefined

- **Hardware API Enhancements** (`useHardware.ts:61`, `AppWrapper.tsx:74-81`)
  - Add `bayIndex` field to API response (currently calculated client-side)
  - Add hashboard.asics with index, row, column data
  - Update hashboard.slot to hashboard.index for TS API consistency
  - Add hashboard.bayIndex field
  - Add miner-info to response with bayCount

- **ASIC Name from API** (`hardwareSlice.ts:98`)
  - Remove client-side ASIC name generation when API provides names
  - Currently using `getAsicName` utility as workaround

- **Remove useHashboardStatus from HashboardTemperature** (`HashboardTemperature.tsx:82-87`)
  - Currently needed to fill gaps between useHardware and useTimeSeries
  - Missing: ASIC rows/columns from useHardware, inlet/outlet temps from useTimeSeries
  - Should be removable once hardware API is enhanced

- **Remove Hardware Data Population from useTimeSeries** (`useTimeSeries.ts:54-55`)
  - useTimeSeries shouldn't need to populate hardware data
  - useHardware hook should provide all necessary hardware information

- **Optimize useHashboardStatus Calls** (`HbTempPreview.tsx:43-49`)
  - Multiple components calling useHashboardStatus for same hashboard
  - Move call higher up component tree to avoid redundant API calls

- **Remove Hardware.tsx useHardware Call** (`Hardware.tsx:16`)
  - Settings Hardware page should read directly from store
  - Remove redundant useHardware call once page is updated

- **Replace SystemContext with Store** (`SystemContextProvider.tsx:13`)
  - Remove SystemContext in favor of zustand store for consistency
  - Add systemInfo slice to unified store

- **Add Preferences to Store** (`HbTempPreview.tsx:58-60`)
  - Move preferences from context to zustand store
  - Clean up redundant unit conversion expressions
  - Share unit types between telemetry and preferences

- **Remove App Wrapper Nesting** (`AppWrapper.tsx:177-178`)
  - Once miner status, system info added to global store
  - Remove complex wrapper component nesting that comprises App.tsx

- **Remove Redundant Utility Functions** (`AsicPopover/utility.ts:1`)
  - Can remove some utils in favor of store hooks (chartDataForMetric, getCurrentValue)
  - Audit and clean up duplicate functionality

- **AsicChart Utility Cleanup** (`AsicChart/utility.ts:1`)
  - Utilities may no longer be needed since moving telemetry to zustand
  - Audit and remove if redundant

- **Use Store getCurrentValue** (`AsicPopover.tsx:52`)
  - Replace formatHashrateWithUnit with getCurrentValue from store
  - Standardize on store utility functions

- **Implement ASIC Cache Map** (`telemetrySlice.ts:287-288`)
  - Add simple cache Map to avoid iterating through all hashboards for every ASIC
  - Would improve performance when processing large numbers of ASICs
