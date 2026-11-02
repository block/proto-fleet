import { Code, ConnectError } from "@connectrpc/connect";
import { type AccountAsset, MiningAssetKind } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";

export const reviewedAssetSelection = (asset: AccountAsset) => ({
  kind: asset.kind,
  id: asset.id,
  expectedRevision: asset.revision,
  ...(asset.kind === MiningAssetKind.MODULE ? { boards: asset.boards } : {}),
});

export const isStaleAssetSelection = (error: unknown) =>
  [Code.Aborted, Code.FailedPrecondition, Code.NotFound].includes(ConnectError.from(error).code);
