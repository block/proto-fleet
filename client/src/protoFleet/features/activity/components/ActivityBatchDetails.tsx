import clsx from "clsx";

import type { GetCommandBatchDeviceResultsResponse } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { formatLabel } from "@/protoFleet/features/activity/utils/formatLabel";
import { Alert, Checkmark, Info } from "@/shared/assets/icons";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { formatActivityTimestamp } from "@/shared/utils/formatTimestamp";

interface ActivityBatchDetailsProps {
  batchId: string;
  data: GetCommandBatchDeviceResultsResponse | null;
  isLoading: boolean;
  error: string | null;
}

const ActivityBatchDetails = ({ data, isLoading, error }: ActivityBatchDetailsProps) => {
  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-6">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  if (error) {
    return (
      <div className="text-intent-critical flex items-center gap-2 py-4 text-200">
        <Alert width="w-3.5" />
        <span>{error}</span>
      </div>
    );
  }

  if (!data) return null;

  if (data.detailsPruned) {
    return (
      <div className="flex items-center gap-2 py-4 text-200 text-text-primary-50">
        <Info width="w-3.5" />
        <span>Per-miner details are no longer available.</span>
      </div>
    );
  }

  const isPending = data.status === "pending" || data.status === "processing";

  return (
    <div className="flex flex-col gap-3 py-3">
      <div className="flex items-center gap-4 text-200 text-text-primary-50">
        <span>{formatLabel(data.commandType.toLowerCase())}</span>
        <span className="capitalize">{data.status}</span>
        <span>
          {data.successCount} succeeded, {data.failureCount} failed
          {data.totalCount > 0 && ` of ${data.totalCount}`}
        </span>
      </div>

      {isPending && data.deviceResults.length === 0 && (
        <div className="py-2 text-200 text-text-primary-50">Results will appear as devices complete.</div>
      )}

      {data.deviceResults.length > 0 && (
        <div className="max-h-80 overflow-y-auto rounded-lg border border-surface-10">
          <table className="w-full text-200">
            <thead className="sticky top-0 bg-surface-5 text-left text-text-primary-50">
              <tr>
                <th className="px-3 py-2 font-medium">Device</th>
                <th className="px-3 py-2 font-medium">Status</th>
                <th className="px-3 py-2 font-medium">Detail</th>
                <th className="px-3 py-2 font-medium">Updated</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-surface-10">
              {data.deviceResults.map((result) => (
                <tr key={result.deviceIdentifier} className="text-text-primary">
                  <td className="text-100 px-3 py-2 font-mono">{result.deviceIdentifier}</td>
                  <td className="px-3 py-2">
                    <span
                      className={clsx(
                        "inline-flex items-center gap-1",
                        result.status === "success" ? "text-intent-success" : "text-intent-critical",
                      )}
                    >
                      {result.status === "success" ? <Checkmark width="w-3" /> : <Alert width="w-3" />}
                      {result.status === "success" ? "Success" : "Failed"}
                    </span>
                  </td>
                  <td className="max-w-xs truncate px-3 py-2 text-text-primary-50">{result.errorMessage ?? "—"}</td>
                  <td className="px-3 py-2 text-text-primary-50">
                    {result.updatedAt ? formatActivityTimestamp(Number(result.updatedAt.seconds)) : "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {data.truncated && (
        <div className="text-200 text-text-primary-50">
          Showing first {data.deviceResults.length} of {data.totalCount} devices.
        </div>
      )}
    </div>
  );
};

export default ActivityBatchDetails;
