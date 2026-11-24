import { type TicketAsset, TicketCategory } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";

export const ticketAssetKey = (asset: TicketAsset) => `${asset.kind}:${asset.id}`;
export const ticketAssetLabel = (asset: TicketAsset) => {
  const kind =
    asset.kind === TicketCategory.HASHBOARD ? "Hashboard" : asset.kind === TicketCategory.MODULE ? "Module" : "Miner";
  const label = asset.label.toLowerCase().startsWith(kind.toLowerCase())
    ? asset.label
    : `${kind} ${asset.label || asset.id}`;
  return `${label} · ID ${asset.id}${asset.kind === TicketCategory.HASHBOARD && asset.moduleId ? ` · Module ${asset.moduleId}` : ""} · ${asset.model || "Model unknown"}`;
};
export const ticketAssetSelection = (asset: TicketAsset) => ({
  kind: asset.kind,
  id: asset.id,
  ...(asset.kind === TicketCategory.HASHBOARD ? { parentModuleId: asset.moduleId } : {}),
});

export const ticketAssetParentKey = (asset: TicketAsset) =>
  asset.kind === TicketCategory.HASHBOARD
    ? asset.moduleId
      ? `module:${asset.moduleId}`
      : `hashboard:${asset.id}`
    : `${asset.kind === TicketCategory.MODULE ? "module" : "miner"}:${asset.id}`;

export const ticketAssetsShareParent = (left: TicketAsset, right: TicketAsset) =>
  left.siteId === right.siteId && ticketAssetParentKey(left) === ticketAssetParentKey(right);

export const ticketCategoryForAssets = (assets: TicketAsset[]) =>
  assets.some((asset) => asset.kind === TicketCategory.MINER)
    ? TicketCategory.MINER
    : assets.some((asset) => asset.kind === TicketCategory.MODULE)
      ? TicketCategory.MODULE
      : TicketCategory.HASHBOARD;

export const ticketCategoryFilters: Record<string, TicketCategory[]> = {
  miner: [TicketCategory.MINER, TicketCategory.MODULE, TicketCategory.HASHBOARD],
  infrastructure: [TicketCategory.INFRASTRUCTURE],
  other: [TicketCategory.ACCOUNT, TicketCategory.OTHER],
};
