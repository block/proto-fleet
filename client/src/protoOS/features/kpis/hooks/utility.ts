import { convertMhSToThS, convertWtoKW } from "@/shared/utils/utility";

export const conversionFns = {
  hashrate: convertMhSToThS,
  powerUsage: convertWtoKW,
  temperature: (value?: number) => (value ? value : 0),
  efficiency: (value?: number) => (value ? value : 0),
} as const;
