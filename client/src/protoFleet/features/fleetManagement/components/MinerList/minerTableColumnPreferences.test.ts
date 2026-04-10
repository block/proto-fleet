import { describe, expect, it } from "vitest";
import { minerCols } from "./constants";
import {
  buildActiveMinerColumns,
  configurableMinerColumns,
  normalizeMinerTableColumnPreferences,
  reorderMinerTableColumns,
  updateMinerTableColumnVisibility,
} from "./minerTableColumnPreferences";

describe("minerTableColumnPreferences", () => {
  it("normalizes persisted preferences, drops invalid entries, and appends missing columns", () => {
    const normalized = normalizeMinerTableColumnPreferences({
      columns: [
        { id: minerCols.model, visible: false },
        { id: minerCols.model, visible: true },
        { id: "unknown" as never, visible: true },
      ],
    });

    expect(normalized.columns[0]).toEqual({ id: minerCols.model, visible: false });
    expect(normalized.columns).toHaveLength(configurableMinerColumns.length);
    expect(normalized.columns.map((column) => column.id)).toEqual([
      minerCols.model,
      ...configurableMinerColumns.filter((columnId) => columnId !== minerCols.model),
    ]);
  });

  it("builds active columns with name fixed first and only visible configurable columns", () => {
    const preferences = updateMinerTableColumnVisibility(
      normalizeMinerTableColumnPreferences({
        columns: configurableMinerColumns.map((columnId) => ({ id: columnId, visible: true })),
      }),
      minerCols.macAddress,
      false,
    );

    expect(buildActiveMinerColumns(preferences)).toEqual([
      minerCols.name,
      ...configurableMinerColumns.filter((columnId) => columnId !== minerCols.macAddress),
    ]);
  });

  it("reorders configurable columns without moving the fixed name column", () => {
    const reordered = reorderMinerTableColumns(
      normalizeMinerTableColumnPreferences({
        columns: configurableMinerColumns.map((columnId) => ({ id: columnId, visible: true })),
      }),
      minerCols.workerName,
      minerCols.groups,
    );

    expect(buildActiveMinerColumns(reordered).slice(0, 4)).toEqual([
      minerCols.name,
      minerCols.workerName,
      minerCols.groups,
      minerCols.model,
    ]);
  });
});
