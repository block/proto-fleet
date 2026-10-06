import MarketDataPanel from "./MarketDataPanel";
import { useMarketData } from "@/protoFleet/api/useMarketData";

export default function MarketDataSection() {
  const { refetch, ...state } = useMarketData();
  return <MarketDataPanel {...state} onRetry={refetch} />;
}
