import { timestampDate } from "@bufbuild/protobuf/wkt";
import MarketDataInfoPopover from "./MarketDataInfoPopover";
import type { GetMarketDataResponse, MarketMetric } from "@/protoFleet/api/generated/marketdata/v1/marketdata_pb";
import SectionHeading from "@/protoFleet/features/dashboard/components/SectionHeading";
import Button from "@/shared/components/Button";
import Stats from "@/shared/components/Stats";

interface MarketDataPanelProps {
  data?: GetMarketDataResponse;
  error: boolean;
  now: number;
  onRetry: () => void;
}

const MAX_SAMPLE_AGE_MS = 60 * 60_000;
const dollars = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 2 });
const hashrate = new Intl.NumberFormat("en-US", { maximumFractionDigits: 2 });

export default function MarketDataPanel({ data, error, now, onRetry }: MarketDataPanelProps) {
  // Hide the section until the server confirms enablement, without a loading flash.
  if (!data?.enabled) return null;
  const staleAfterMs = 2 * (data.refreshIntervalSeconds || 60) * 1000;
  const stat = (label: string, metric: MarketMetric | undefined, format: (value: number) => string, units: string) => {
    const retrieved = metric?.retrievedAt ? timestampDate(metric.retrievedAt) : undefined;
    const age = retrieved ? Math.max(0, now - retrieved.getTime()) : Infinity;
    const available =
      metric && retrieved && Number.isFinite(metric.value) && metric.value > 0 && age < MAX_SAMPLE_AGE_MS;
    const stale = available && (error || metric.stale || age >= staleAfterMs);
    return {
      label,
      value: available ? format(metric.value) : "—",
      units: available ? units : undefined,
      text: !available ? (
        "Unavailable"
      ) : (
        <>
          {stale ? "Stale · " : ""}
          Retrieved{" "}
          <time dateTime={retrieved.toISOString()} title={retrieved.toLocaleString()}>
            {age < 60_000 ? "<1 min ago" : `${Math.floor(age / 60_000)} min ago`}
          </time>
        </>
      ),
    };
  };

  return (
    <section className="px-6 pb-6 laptop:px-10" aria-label="Market data" data-testid="market-data-panel">
      <div className="flex items-center gap-2">
        <SectionHeading heading="Market data" />
        <MarketDataInfoPopover />
      </div>
      <div className="mt-4 rounded-xl bg-surface-elevated-base p-6 shadow-100">
        <Stats
          stats={[
            stat("Bitcoin price", data.bitcoinPriceUsd, (value) => dollars.format(value), "USD"),
            stat(
              "Hashprice (estimate)",
              data.estimatedHashpriceUsdPerPhDay,
              (value) => dollars.format(value),
              "USD/PH/day",
            ),
            stat("Network hashrate", data.networkHashrateHs, (value) => hashrate.format(value / 1e18), "EH/s"),
          ]}
          grid="grid-cols-1 tablet:grid-cols-3"
          gap="gap-6"
          padding=""
          size="medium"
        />
      </div>
      {error ? (
        <div className="mt-3 flex flex-wrap items-center gap-3 text-300 text-text-primary-70" role="status">
          <span>Market data couldn’t refresh.</span>
          <Button variant="textOnly" text="Retry" onClick={onRetry} />
        </div>
      ) : null}
    </section>
  );
}
