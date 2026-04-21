export interface ChartData {
  datetime: number;
  [key: string]: number | null; // Dynamic keys for hashboard serials, "miner", etc.
}
