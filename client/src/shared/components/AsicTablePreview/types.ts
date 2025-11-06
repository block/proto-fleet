export interface AsicData {
  row: number;
  col: number;
  value: number | null;
}

export interface AsicTablePreviewProps {
  asics: AsicData[];
  min?: number; // Start of scale (default: 0°C)
  warningThreshold?: number; // Warning threshold (default: 65°C)
  dangerThreshold?: number; // Danger threshold (default: 82°C)
  criticalThreshold?: number; // Critical threshold (default: 90°C)

  // Color props to avoid CSS variable side effects
  colors?: {
    normal: string; // Default: intent-info (#0096D1)
    warning: string; // Default: intent-warning (#FD8A00)
    critical: string; // Default: intent-critical (#FA2B37)
    empty: string; // Default: core-primary-5 (#F2F2F2)
  };

  className?: string;
}

export interface AsicCellProps {
  row: number;
  col: number;
  value: number | null;
  min: number;
  warningThreshold: number;
  criticalThreshold: number;
  dangerThreshold: number;
  colors: {
    normal: string;
    warning: string;
    critical: string;
    empty: string;
  };
}
