import { useCallback, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { siteMapClient } from "@/protoFleet/api/clients";
import {
  ExportSiteMapCsvRequestSchema,
  ImportSiteMapCsvRequestSchema,
  OmissionMode,
} from "@/protoFleet/api/generated/sitemap/v1/sitemap_pb";
import type { ImportSiteMapCsvResponse } from "@/protoFleet/api/generated/sitemap/v1/sitemap_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";
import { pushToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";
import { downloadBlob, getFileName } from "@/shared/utils/utility";

const MIN_LOADING_MS = 400;

const sleep = (ms: number) => new Promise((resolve) => window.setTimeout(resolve, ms));

type ImportSiteMapCsvOptions = {
  file: File;
  omissionMode: OmissionMode;
  dryRun: boolean;
  commitToken?: string;
};

const useSiteMapCsv = () => {
  const [isExporting, setIsExporting] = useState(false);
  const [isImporting, setIsImporting] = useState(false);
  const isExportingRef = useRef(false);
  const isImportingRef = useRef(false);
  const { handleAuthErrors } = useAuthErrors();

  const exportSiteMapCsv = useCallback(async () => {
    if (isExportingRef.current) {
      return;
    }

    const startedAt = Date.now();
    isExportingRef.current = true;
    setIsExporting(true);

    try {
      const chunks: Uint8Array<ArrayBuffer>[] = [];

      for await (const chunk of siteMapClient.exportSiteMapCsv(create(ExportSiteMapCsvRequestSchema, {}))) {
        chunks.push(new Uint8Array(chunk.csvData));
      }

      const blob = new Blob(chunks, { type: "text/csv;charset=utf-8;" });
      downloadBlob(blob, getFileName("proto-fleet-site-map"));
    } catch (error) {
      handleAuthErrors({
        error,
        onError: (err) => {
          console.error("Error exporting site map CSV:", err);
          const message = getErrorMessage(err, "Failed to export site map. Please try again.");
          pushToast({
            status: TOAST_STATUSES.error,
            message,
          });
        },
      });
    } finally {
      const elapsedMs = Date.now() - startedAt;
      const remainingMs = MIN_LOADING_MS - elapsedMs;
      if (remainingMs > 0) {
        await sleep(remainingMs);
      }
      isExportingRef.current = false;
      setIsExporting(false);
    }
  }, [handleAuthErrors]);

  const importSiteMapCsv = useCallback(
    async ({ file, omissionMode, dryRun, commitToken }: ImportSiteMapCsvOptions): Promise<ImportSiteMapCsvResponse> => {
      if (isImportingRef.current) {
        throw new Error("A site map import is already in progress.");
      }

      const startedAt = Date.now();
      isImportingRef.current = true;
      setIsImporting(true);

      try {
        const csvData = new Uint8Array(await file.arrayBuffer());
        return await siteMapClient.importSiteMapCsv(
          create(ImportSiteMapCsvRequestSchema, {
            csvData,
            omissionMode,
            dryRun,
            commitToken: commitToken ?? "",
          }),
        );
      } catch (error) {
        handleAuthErrors({
          error,
          onError: (err) => {
            console.error("Error importing site map CSV:", err);
          },
        });
        const importError = new Error(
          getErrorMessage(error, "Failed to import site map. Please try again."),
        ) as Error & {
          cause?: unknown;
        };
        importError.cause = error;
        throw importError;
      } finally {
        const elapsedMs = Date.now() - startedAt;
        const remainingMs = MIN_LOADING_MS - elapsedMs;
        if (remainingMs > 0) {
          await sleep(remainingMs);
        }
        isImportingRef.current = false;
        setIsImporting(false);
      }
    },
    [handleAuthErrors],
  );

  return {
    exportSiteMapCsv,
    importSiteMapCsv,
    isExportingSiteMapCsv: isExporting,
    isImportingSiteMapCsv: isImporting,
  };
};

export default useSiteMapCsv;
