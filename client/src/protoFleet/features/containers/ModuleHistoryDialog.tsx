import { useState } from "react";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import ModuleActionsMenu from "./ModuleActionsMenu";
import { ModuleStatus } from "./ModuleStatus";
import { fleetInventoryClient } from "@/protoFleet/api/clients";
import type { InventoryItem, ModuleSample } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import Button, { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import { usePoll } from "@/shared/hooks/usePoll";

export function ModuleHistoryDialog({
  item,
  onDismiss,
  onRefresh,
}: {
  item: InventoryItem;
  onDismiss: () => void;
  onRefresh?: () => void;
}) {
  const [window] = useState(() => ({ end: new Date(), start: new Date(Date.now() - 60 * 60 * 1000) }));
  const [cursor, setCursor] = useState("");
  const [revision, setRevision] = useState(0);
  const [result, setResult] = useState<{ samples: ModuleSample[]; next: string; error?: string }>();
  usePoll({
    params: `${cursor}:${revision}`,
    fetchData: async (signal) => {
      try {
        const response = await fleetInventoryClient.listModuleHistory(
          {
            moduleId: item.ref?.id,
            startTime: timestampFromDate(window.start),
            endTime: timestampFromDate(window.end),
            pageSize: 100,
            pageToken: cursor,
          },
          { signal },
        );
        if (!signal.aborted) setResult({ samples: response.samples, next: response.nextPageToken });
      } catch (error) {
        if (!signal.aborted) setResult({ samples: [], next: "", error: getErrorMessage(error) });
      }
    },
  });
  return (
    <Dialog
      open
      title={item.name}
      onDismiss={onDismiss}
      testId="module-history"
      buttons={[{ text: "Close", variant: variants.secondary, onClick: onDismiss }]}
    >
      <div className="flex flex-wrap items-center gap-2">
        <span>{[item.facilityName, item.groupName].filter(Boolean).join(" / ")}</span>
        <ModuleStatus state={item.state} />
        <ModuleActionsMenu
          modules={[item]}
          onRefresh={() => {
            setCursor("");
            setResult(undefined);
            setRevision((value) => value + 1);
            onRefresh?.();
          }}
        />
      </div>
      <p className="py-3 text-text-primary-70">Module telemetry · past hour</p>
      {result?.error ? (
        <p role="alert">{result.error}</p>
      ) : !result ? (
        <p>Loading…</p>
      ) : result.samples.length === 0 ? (
        <p>No telemetry recorded in this period.</p>
      ) : (
        <div className="max-h-80 overflow-auto">
          <table className="w-full text-left text-200">
            <thead>
              <tr>
                <th>Observed</th>
                <th>TH/s</th>
                <th>kW</th>
                <th>°C</th>
              </tr>
            </thead>
            <tbody>
              {result.samples.map((sample, index) => (
                <tr key={index}>
                  <td>{new Date(Number(sample.observedAt?.seconds ?? 0n) * 1000).toLocaleTimeString()}</td>
                  <td>{sample.hashrateHs === undefined ? "—" : (sample.hashrateHs / 1e12).toFixed(2)}</td>
                  <td>{sample.powerW === undefined ? "—" : (sample.powerW / 1000).toFixed(2)}</td>
                  <td>{sample.temperatureC?.toFixed(1) ?? "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {result?.next ? (
        <Button
          variant={variants.secondary}
          text="Next samples"
          onClick={() => {
            setCursor(result.next);
            setResult(undefined);
          }}
        />
      ) : null}
    </Dialog>
  );
}
