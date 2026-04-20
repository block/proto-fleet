import { useCallback, useRef, useState } from "react";
import { activityClient } from "@/protoFleet/api/clients";
import type { ActivityFilter } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import { useAuthErrors } from "@/protoFleet/store";
import { pushToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";
import { downloadBlob, getFileName } from "@/shared/utils/utility";

const MIN_EXPORT_LOADING_MS = 400;

const sleep = (ms: number) => new Promise((resolve) => window.setTimeout(resolve, ms));

export function useExportActivity() {
  const [isExporting, setIsExporting] = useState(false);
  const isExportingRef = useRef(false);
  const { handleAuthErrors } = useAuthErrors();

  const handleExportCsv = useCallback(
    async (filter?: ActivityFilter) => {
      if (isExportingRef.current) return;

      const startedAt = Date.now();
      isExportingRef.current = true;
      setIsExporting(true);

      try {
        const chunks: Uint8Array<ArrayBuffer>[] = [];

        for await (const chunk of activityClient.exportActivities({ filter })) {
          chunks.push(new Uint8Array(chunk.chunk));
        }

        const blob = new Blob(chunks, { type: "text/csv;charset=utf-8;" });
        downloadBlob(blob, getFileName("activity-export"));
      } catch (error) {
        handleAuthErrors({
          error,
          onError: (err) => {
            console.error("Error exporting activities:", err);
            pushToast({
              status: TOAST_STATUSES.error,
              message: "Failed to export activities. Please try again.",
            });
          },
        });
      } finally {
        const elapsedMs = Date.now() - startedAt;
        const remainingMs = MIN_EXPORT_LOADING_MS - elapsedMs;
        if (remainingMs > 0) {
          await sleep(remainingMs);
        }
        isExportingRef.current = false;
        setIsExporting(false);
      }
    },
    [handleAuthErrors],
  );

  return { exportCsv: handleExportCsv, isExportingCsv: isExporting };
}
