import { Code, ConnectError } from "@connectrpc/connect";
import { reportingImportBatches, type ReportingRegister } from "./reportingInputs";
import type { ReportingFact } from "@/protoFleet/api/generated/hosted/v1/reporting_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";

export type ReportingImportProgress = {
  status: "running" | "complete" | "cancelled" | "failed" | "unknown";
  confirmedRows: number;
  revision: bigint;
  pending: ReportingFact[];
  error?: string;
};
export const runReportingImport = async (
  register: ReportingRegister,
  facts: ReportingFact[],
  revision: bigint,
  submit: (batch: ReportingFact[], revision: bigint) => Promise<bigint>,
  cancelled: () => boolean,
  changed: (progress: ReportingImportProgress) => void,
) => {
  let progress: ReportingImportProgress = { status: "running", confirmedRows: 0, revision, pending: [] };
  for (const batch of reportingImportBatches(register, facts)) {
    if (cancelled()) {
      progress = { ...progress, status: "cancelled" };
      changed(progress);
      return progress;
    }
    progress = { ...progress, pending: batch };
    changed(progress);
    try {
      const next = await submit(batch, progress.revision);
      if (next <= progress.revision) throw new Error("The server did not confirm the new input revision.");
      progress = { ...progress, confirmedRows: progress.confirmedRows + batch.length, revision: next, pending: [] };
      changed(progress);
    } catch (cause) {
      const code = ConnectError.from(cause).code;
      const rejected = [
        Code.InvalidArgument,
        Code.PermissionDenied,
        Code.Unauthenticated,
        Code.Aborted,
        Code.FailedPrecondition,
        Code.NotFound,
        Code.ResourceExhausted,
      ].includes(code);
      progress = {
        ...progress,
        status: rejected ? "failed" : "unknown",
        pending: rejected ? [] : batch,
        error: getErrorMessage(cause),
      };
      changed(progress);
      return progress;
    }
  }
  progress = { ...progress, status: "complete" };
  changed(progress);
  return progress;
};
