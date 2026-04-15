import { create } from "@bufbuild/protobuf";
import { StreamCommandBatchUpdatesRequestSchema } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { useMinerCommand } from "@/protoFleet/api/useMinerCommand";

type StreamCommandBatchUpdates = ReturnType<typeof useMinerCommand>["streamCommandBatchUpdates"];

export type WorkerNameBatchResult = {
  streamFailed: boolean;
  successCount: number;
  failedCount: number;
  successDeviceIds: string[];
};

export async function waitForWorkerNameBatchResult(
  streamCommandBatchUpdates: StreamCommandBatchUpdates,
  batchIdentifier: string,
): Promise<WorkerNameBatchResult> {
  const streamAbortController = new AbortController();
  const batchResult: WorkerNameBatchResult = {
    streamFailed: false,
    successCount: 0,
    failedCount: 0,
    successDeviceIds: [],
  };
  let totalCount = 0;

  await streamCommandBatchUpdates({
    streamRequest: create(StreamCommandBatchUpdatesRequestSchema, {
      batchIdentifier,
    }),
    streamAbortController,
    onStreamData: (streamResponse) => {
      totalCount = Number(streamResponse.status?.commandBatchDeviceCount?.total || 0);
      batchResult.successCount = Number(streamResponse.status?.commandBatchDeviceCount?.success || 0);
      batchResult.failedCount = Number(streamResponse.status?.commandBatchDeviceCount?.failure || 0);
      batchResult.successDeviceIds = streamResponse.status?.commandBatchDeviceCount?.successDeviceIdentifiers || [];

      if (batchResult.successCount + batchResult.failedCount === totalCount && totalCount > 0) {
        streamAbortController.abort();
      }
    },
    onError: () => {
      batchResult.streamFailed = true;
    },
  });

  return batchResult;
}
