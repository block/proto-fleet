import { useCallback, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import {
  CsvTemperatureUnit,
  ExportMinerListCsvRequestSchema,
  type MinerListFilter,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useAuthErrors, useTemperatureUnit } from "@/protoFleet/store";
import { pushToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";
import { downloadBlob, getFileName } from "@/shared/utils/utility";

type UseExportMinerListCsvOptions = {
  filter?: MinerListFilter;
};

const MIN_EXPORT_LOADING_MS = 400;

const sleep = (ms: number) => new Promise((resolve) => window.setTimeout(resolve, ms));

const useExportMinerListCsv = ({ filter }: UseExportMinerListCsvOptions) => {
  const [isExporting, setIsExporting] = useState(false);
  const isExportingRef = useRef(false);
  const temperatureUnit = useTemperatureUnit();
  const { handleAuthErrors } = useAuthErrors();

  const handleExportCsv = useCallback(async () => {
    if (isExportingRef.current) {
      return;
    }

    const startedAt = Date.now();
    isExportingRef.current = true;
    setIsExporting(true);

    try {
      const chunks: Uint8Array<ArrayBuffer>[] = [];

      for await (const chunk of fleetManagementClient.exportMinerListCsv(
        create(ExportMinerListCsvRequestSchema, {
          filter,
          temperatureUnit: temperatureUnit === "F" ? CsvTemperatureUnit.FAHRENHEIT : CsvTemperatureUnit.CELSIUS,
        }),
      )) {
        chunks.push(new Uint8Array(chunk.csvData));
      }

      const blob = new Blob(chunks, { type: "text/csv;charset=utf-8;" });
      downloadBlob(blob, getFileName("proto-fleet-miner-snapshot"));
    } catch (error) {
      handleAuthErrors({
        error,
        onError: (err) => {
          console.error("Error exporting miner list CSV:", err);
          pushToast({
            status: TOAST_STATUSES.error,
            message: "Failed to export miners. Please try again.",
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
  }, [filter, handleAuthErrors, temperatureUnit]);

  return {
    exportCsv: handleExportCsv,
    isExportingCsv: isExporting,
  };
};

export default useExportMinerListCsv;
