import { useEffect, useRef, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";
import { sha256 } from "@noble/hashes/sha2.js";
import { bytesToHex } from "@noble/hashes/utils.js";

import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { isAuthOrPermissionError } from "@/protoFleet/api/requestErrors";
import { useFleetStore } from "@/protoFleet/store";
import { downloadBlob } from "@/shared/utils/utility";

type ReportChunk = { chunk: Uint8Array; filename: string; mediaType: string; sha256: string };
type ReportFilePicker = (options: { suggestedName: string }) => Promise<{
  createWritable: () => Promise<FileSystemWritableFileStream>;
}>;
export const REPORT_DOWNLOAD_BUFFER_BYTES = 32 * 1024 * 1024;
export type ReportFile = "report.html" | "evidence.zip";
export type HostReportFile = ReportFile | "source-inputs.json";
export const useReportDownload = <File extends string = ReportFile>(
  identity: string,
  signal: AbortSignal,
  stream: (name: File, signal: AbortSignal) => AsyncIterable<ReportChunk>,
  onDenied: () => void,
) => {
  const controller = useRef<AbortController | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    const lifetime = new AbortController();
    controller.current = lifetime;
    return () => lifetime.abort();
  }, [identity, signal]);
  const download = async (name: File) => {
    if (busy || !controller.current || signal.aborted) return;
    const operation = new AbortController();
    const requestSignal = AbortSignal.any([signal, controller.current.signal, operation.signal]);
    const generation = useFleetStore.getState().auth.sessionGeneration;
    const current = () => !requestSignal.aborted && generation === useFleetStore.getState().auth.sessionGeneration;
    const unsubscribe = useFleetStore.subscribe((state) => {
      if (state.auth.sessionGeneration !== generation) operation.abort();
    });
    const parts: ArrayBuffer[] = [];
    const hash = sha256.create();
    let writable: FileSystemWritableFileStream | undefined;
    let committed = false;
    let aborting: Promise<void> | undefined;
    const abortFile = () => {
      if (writable && !committed) aborting ??= writable.abort().catch(() => undefined);
      return aborting;
    };
    const onAbort = () => void abortFile();
    requestSignal.addEventListener("abort", onAbort);
    let choosingFile = false;
    setBusy(true);
    setError("");
    try {
      const picker = (window as Window & { showSaveFilePicker?: ReportFilePicker }).showSaveFilePicker;
      if (window.isSecureContext && typeof picker === "function") {
        choosingFile = true;
        const handle = await picker.call(window, { suggestedName: name });
        choosingFile = false;
        if (!current()) return;
        writable = await handle.createWritable();
        if (!current()) return;
      }
      let received = 0;
      let metadata: Omit<ReportChunk, "chunk"> | undefined;
      for await (const item of stream(name, requestSignal)) {
        if (!current()) return;
        if (!item.filename || !item.mediaType || !/^[a-f0-9]{64}$/i.test(item.sha256))
          throw new Error("The report download has invalid integrity metadata. Retry the download.");
        if (
          metadata &&
          (metadata.filename !== item.filename ||
            metadata.mediaType !== item.mediaType ||
            metadata.sha256 !== item.sha256)
        )
          throw new Error("The report download metadata changed. Retry the download.");
        metadata = { filename: item.filename, mediaType: item.mediaType, sha256: item.sha256 };
        if (!writable && item.chunk.byteLength > REPORT_DOWNLOAD_BUFFER_BYTES - received)
          throw new Error(
            "This file exceeds this browser's 32 MiB buffered download limit. Ask your host for the complete file.",
          );
        hash.update(item.chunk);
        if (writable) await writable.write(new Uint8Array(item.chunk));
        else parts.push(new Uint8Array(item.chunk).buffer);
        received += item.chunk.byteLength;
      }
      if (!current()) return;
      if (!received || !metadata) throw new Error("The report download was empty. Retry the download.");
      if (bytesToHex(hash.digest()) !== metadata.sha256.toLowerCase())
        throw new Error("The report download failed its integrity check. Retry the download.");
      if (writable) {
        await writable.close();
        committed = true;
      } else downloadBlob(new Blob(parts, { type: metadata.mediaType }), metadata.filename);
    } catch (cause) {
      if (choosingFile && cause instanceof DOMException && cause.name === "AbortError") return;
      if (current()) {
        setError(getErrorMessage(cause));
        const failure = ConnectError.from(cause);
        if (isAuthOrPermissionError(failure)) onDenied();
        if (failure.code === Code.Unauthenticated) useFleetStore.getState().auth.logout();
      }
    } finally {
      const active = current();
      unsubscribe();
      operation.abort();
      await abortFile();
      requestSignal.removeEventListener("abort", onAbort);
      hash.destroy();
      parts.length = 0;
      if (active) setBusy(false);
    }
  };
  return { download, busy, error };
};
