import { useEffect, useMemo, useState } from "react";
import { timestampMs } from "@bufbuild/protobuf/wkt";
import { type BoardAsicStreamTarget, type OpenBoardAsicStream, useBoardAsicStream } from "./useBoardAsicStream";
import type { StreamBoardASICReadingsResponse } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { useTemperatureUnit } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import AsicTablePreview, { type AsicData } from "@/shared/components/AsicTablePreview";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal";
import Stats from "@/shared/components/Stats";
import { containerAsicPlacement } from "@/shared/utils/containerAsicLayout";
import { convertCtoF, formatHashrateOrDash } from "@/shared/utils/telemetryFormat";

// The controller samples about once per second.
export const BOARD_ASIC_STALE_MS = 10_000;

const clock = (ms: number) =>
  new Date(ms).toLocaleTimeString([], { hour: "numeric", minute: "2-digit", second: "2-digit" });

const summarize = (frame: StreamBoardASICReadingsResponse) => {
  const temperatures = frame.asics.map((asic) => asic.temperatureC).filter(Number.isFinite);
  const quarantined = frame.asics.filter((asic) => asic.thermalQuarantined === true).length;
  return {
    reporting: frame.asics.length,
    // reduce, not a spread: a misreported frame may be far larger than a board.
    high: temperatures.length ? temperatures.reduce((high, t) => Math.max(high, t)) : undefined,
    average: temperatures.length ? temperatures.reduce((sum, t) => sum + t, 0) / temperatures.length : undefined,
    hashrateTh: frame.asics.reduce((sum, asic) => sum + asic.hashrateGhs, 0) / 1_000,
    quarantined: frame.asics.some((asic) => asic.thermalQuarantined !== undefined) ? quarantined : undefined,
  };
};

const heatmap = (frame: StreamBoardASICReadingsResponse): AsicData[] => {
  const place = containerAsicPlacement(
    frame.asics.flatMap((asic) => (asic.position ? [asic.position] : [])),
    frame.asicsPerStack,
  );
  return frame.asics.flatMap((asic) => {
    if (!asic.position) return [];
    const { row, column } = place(asic.position);
    return row === undefined || column === undefined ? [] : [{ row, col: column, value: asic.temperatureC }];
  });
};

export default function BoardAsicReadingsDialog({
  board,
  position,
  onClose,
  openStream,
}: {
  board: BoardAsicStreamTarget;
  position: number;
  onClose: () => void;
  /** Overrides the Fleet stream, for stories and tests. */
  openStream?: OpenBoardAsicStream;
}) {
  const unit = useTemperatureUnit();
  const { frame, freshAt, error, final } = useBoardAsicStream(board, openStream);
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1_000);
    return () => clearInterval(timer);
  }, []);
  const summary = useMemo(() => (frame ? summarize(frame) : undefined), [frame]);
  const cells = useMemo(() => (frame ? heatmap(frame) : []), [frame]);
  const stale = !!frame && (!!error || (freshAt !== undefined && now - freshAt > BOARD_ASIC_STALE_MS));
  const observed = frame?.observedAt ? timestampMs(frame.observedAt) : freshAt;
  const temperature = (value: number | undefined) =>
    value === undefined ? "—" : `${(unit === "F" ? convertCtoF(value) : value).toFixed(1)} °${unit}`;
  const status = !frame
    ? error
      ? undefined
      : "Connecting to the controller…"
    : stale
      ? `Not live. Last reading at ${observed === undefined ? "an unknown time" : clock(observed)}.`
      : `Live. Last reading at ${observed === undefined ? "an unknown time" : clock(observed)}.`;

  return (
    <Modal
      open
      testId="board-asic-readings"
      title={`Board ${position} ASIC readings`}
      dialogLabel={`Board ${position} ASIC readings`}
      manageFocus
      onDismiss={onClose}
      buttons={[{ text: "Done", variant: variants.primary, onClick: onClose }]}
    >
      <div className="flex flex-col gap-4">
        {status ? (
          <p role="status" className="text-300 text-text-primary-70">
            {status}
          </p>
        ) : null}
        {error ? (
          <Callout
            intent={final ? "danger" : "warning"}
            prefixIcon={<Alert />}
            title={final ? "Live ASIC readings stopped" : "Reconnecting to live ASIC readings"}
            subtitle={error}
          />
        ) : null}
        {summary ? (
          <div className={stale ? "opacity-50" : undefined} data-testid="board-asic-readings-current">
            <Stats
              size="small"
              padding="pb-2"
              grid="grid-cols-5 phone:grid-cols-2"
              stats={[
                { label: "ASICs", value: String(summary.reporting), preformatted: true },
                { label: "ASIC high", value: temperature(summary.high), preformatted: true },
                { label: "ASIC avg", value: temperature(summary.average), preformatted: true },
                { label: "Hashrate", value: formatHashrateOrDash(summary.hashrateTh), preformatted: true },
                {
                  label: "Quarantined",
                  value: summary.quarantined === undefined ? "—" : String(summary.quarantined),
                  preformatted: true,
                },
              ]}
            />
            {cells.length ? (
              <AsicTablePreview asics={cells} rowHeight={16} />
            ) : (
              <p className="text-300 text-text-primary-70">The controller has not reported ASIC positions.</p>
            )}
          </div>
        ) : null}
      </div>
    </Modal>
  );
}
