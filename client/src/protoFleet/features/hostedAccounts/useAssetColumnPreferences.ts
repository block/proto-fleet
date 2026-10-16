import { useState } from "react";
import { type AssetColumn, assetColumnTitles } from "./components/accountAssetColumns";
import type { ColumnPreference } from "@/shared/components/ManageColumnsModal";

export const defaultAssetColumns: ColumnPreference<AssetColumn>[] = (Object.keys(assetColumnTitles) as AssetColumn[])
  .filter((id) => id !== "name")
  .map((id) => ({ id, visible: true }));

const normalize = (value: unknown): ColumnPreference<AssetColumn>[] => {
  const result: ColumnPreference<AssetColumn>[] = [];
  const saved = Array.isArray(value) ? value : [];
  for (const item of [...saved, ...defaultAssetColumns]) {
    if (
      !item ||
      typeof item !== "object" ||
      typeof item.id !== "string" ||
      !defaultAssetColumns.some((column) => column.id === item.id) ||
      result.some((column) => column.id === item.id)
    )
      continue;
    result.push({ id: item.id as AssetColumn, visible: item.visible !== false });
  }
  return result;
};
const read = (key: string) => {
  try {
    return normalize(JSON.parse(localStorage.getItem(key) ?? "null"));
  } catch {
    return normalize(null);
  }
};
export function useAssetColumnPreferences(username: string, accountId: string) {
  const key = `proto-fleet-account-asset-columns:${username}:${accountId}`;
  const [state, setState] = useState(() => ({ key, columns: read(key) }));
  if (state.key !== key) {
    setState({ key, columns: read(key) });
  }
  return {
    columns: state.key === key ? state.columns : read(key),
    save: (value: ColumnPreference<AssetColumn>[]) => {
      const columns = normalize(value);
      setState({ key, columns });
      try {
        localStorage.setItem(key, JSON.stringify(columns));
      } catch {
        return;
      }
    },
  };
}
