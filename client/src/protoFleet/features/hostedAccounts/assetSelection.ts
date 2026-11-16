import { Code, ConnectError } from "@connectrpc/connect";
import { FleetErrorDetailsSchema } from "@/protoFleet/api/generated/common/v1/common_pb";
import {
  type AccountAsset,
  AssetAssignmentErrorCode,
  MiningAssetKind,
} from "@/protoFleet/api/generated/hosted/v1/hosted_pb";

export const reviewedAssetSelection = (asset: AccountAsset) => ({
  kind: asset.kind,
  id: asset.id,
  expectedRevision: asset.revision,
  ...(asset.kind === MiningAssetKind.MODULE ? { boards: asset.boards } : {}),
});

export const isStaleAssetSelection = (cause: unknown) => {
  const error = ConnectError.from(cause);
  if (
    error.code === Code.FailedPrecondition &&
    error
      .findDetails(FleetErrorDetailsSchema)
      .some(
        (detail) =>
          detail.code.case === "endpoint" && detail.code.value === AssetAssignmentErrorCode.MINING_GROUPS_NOT_READY,
      )
  )
    return false;
  return [Code.Aborted, Code.FailedPrecondition, Code.NotFound].includes(error.code);
};
