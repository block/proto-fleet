import { useState } from "react";
import { create } from "@bufbuild/protobuf";
import { type PlantAlarmItem, plantAlarmItems } from "../plantAlarms";
import { errorQueryClient } from "@/protoFleet/api/clients";
import {
  type ErrorMessage,
  MinerError,
  QueryRequestSchema,
  ResultView,
} from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { usePoll } from "@/shared/hooks/usePoll";

const MAX_PAGES = 5;

/**
 * Open plant alarms for a container, read from the errors API. The plugin
 * reports each alarm as an unmapped vendor error on component `plant-alarm:<id>`;
 * the component prefix is matched here because the filter only takes exact ids.
 * `observationKey` refetches whenever the container reports a new plant reading.
 */
export function usePlantAlarmErrors(deviceIdentifier: string, observationKey: string, enabled = true) {
  const [result, setResult] = useState<{ key: string; items?: PlantAlarmItem[]; failed?: boolean }>();
  const key = `${deviceIdentifier}:${observationKey}`;
  usePoll({
    enabled: enabled && !!deviceIdentifier,
    params: key,
    fetchData: async (signal) => {
      try {
        const errors: ErrorMessage[] = [];
        let pageToken = "";
        for (let page = 0; page < MAX_PAGES; page++) {
          const response = await errorQueryClient.query(
            create(QueryRequestSchema, {
              resultView: ResultView.ERROR,
              filter: {
                simple: { deviceIdentifiers: [deviceIdentifier], canonicalErrors: [MinerError.VENDOR_ERROR_UNMAPPED] },
                includeClosed: false,
              },
              pageSize: 1000,
              pageToken,
            }),
            { signal },
          );
          if (response.result.case === "errors") errors.push(...response.result.value.items);
          pageToken = response.nextPageToken;
          if (!pageToken) break;
        }
        if (!signal.aborted) setResult({ key, items: plantAlarmItems(errors) });
      } catch {
        if (!signal.aborted) setResult({ key, failed: true });
      }
    },
  });
  const current = result?.key.startsWith(`${deviceIdentifier}:`) ? result : undefined;
  return { alarms: current?.items ?? [], failed: current?.failed === true, loaded: current !== undefined };
}
