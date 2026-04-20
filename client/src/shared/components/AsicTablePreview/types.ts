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

  // Color props (defaults use CSS custom properties for theme support)
  colors?: {
    normal: string; // Default: var(--color-intent-info-fill)
    warning: string; // Default: var(--color-intent-warning-fill)
    critical: string; // Default: var(--color-intent-critical-fill)
    empty: string; // Default: var(--color-core-primary-5)
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
