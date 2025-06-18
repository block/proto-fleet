/**
 * Time series data point with a timestamp and value
 */
export interface TimeSeriesDataPoint {
  /** Unix time epoch */
  datetime?: number;
  /** Value of data at the given datetime */
  value?: number;
}

/**
 * Aggregate statistics
 */
export interface AggregateStats {
  /** Average value in data */
  avg?: number;
  /** Maximum value in data */
  max?: number;
  /** Minimum value in data */
  min?: number;
}
