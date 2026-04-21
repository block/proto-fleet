import { createContext, useContext, useMemo } from "react";
import type { ReactNode } from "react";

export type SelectedMetric = "hashrate" | "frequency" | "voltage" | "temperature";

interface AsicMetricContextValue {
  selectedMetric: SelectedMetric;
}

const AsicMetricContext = createContext<AsicMetricContextValue | undefined>(undefined);

interface AsicMetricProviderProps {
  children: ReactNode;
  selectedMetric: SelectedMetric;
}

export const AsicMetricProvider = ({ children, selectedMetric }: AsicMetricProviderProps) => {
  const value = useMemo(() => ({ selectedMetric }), [selectedMetric]);

  return <AsicMetricContext.Provider value={value}>{children}</AsicMetricContext.Provider>;
};

// eslint-disable-next-line react-refresh/only-export-components
export const useAsicMetric = (): AsicMetricContextValue => {
  const context = useContext(AsicMetricContext);
  if (context === undefined) {
    throw new Error("useAsicMetric must be used within an AsicMetricProvider");
  }
  return context;
};
