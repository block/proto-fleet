import { AsicData } from "@/protoOS/store";
import { getAsicId } from "@/protoOS/store";

// Helper function to create mock ASIC data
const createMockAsic = (id: number, row: number, column: number, tempC: number, hashrateGhs: number): AsicData => ({
  // Hardware data
  id: getAsicId("421FS23103000005", id.toString()),
  hashboardSerial: "421FS23103000005",
  row,
  column,
  index: id,
  hashboardIndex: 2,

  // Telemetry data (simplified for stories)
  temperature: {
    latest: {
      value: tempC,
      units: "C" as const,
    },
    timeSeries: {
      units: "C" as const,
      values: [tempC],
      aggregates: {
        avg: { value: tempC, units: "C" },
        min: { value: tempC, units: "C" },
        max: { value: tempC, units: "C" },
      },
      startTime: Date.now() - 3600000,
      endTime: Date.now(),
    },
  },
  hashrate: {
    latest: {
      value: hashrateGhs,
      units: "GH/s" as const,
    },
    timeSeries: {
      units: "GH/s" as const,
      values: [hashrateGhs],
      aggregates: {
        avg: { value: hashrateGhs, units: "GH/s" },
        min: { value: hashrateGhs, units: "GH/s" },
        max: { value: hashrateGhs, units: "GH/s" },
      },
      startTime: Date.now() - 3600000,
      endTime: Date.now(),
    },
  },
});

// Mock hashboard stats with simplified ASIC grid (10x10)
export const mockHashboardStats = {
  hb_sn: "421FS23103000005",
  slot: 2,
  status: "Running",
  power_usage_watts: 783,
  voltage_mv: 13.699999809265137,
  avg_asic_temp_c: 59.71818161010742,
  hashrate_ghs: 27713.266,
  ideal_hashrate_ghs: 28320,
  efficiency_jth: 0,
  asics: [
    // Create a 10x10 grid of ASICs for the story
    ...Array.from({ length: 100 }, (_, index) => {
      const row = Math.floor(index / 10);
      const column = index % 10;
      const tempVariation = Math.random() * 10 + 55; // 55-65°C range
      const hashrateVariation = Math.random() * 40 + 260; // 260-300 GH/s range
      return createMockAsic(index, row, column, Math.round(tempVariation), Math.round(hashrateVariation * 100) / 100);
    }),
  ],
};
