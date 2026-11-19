import { describe, expect, it } from "vitest";
import { minerCols } from "./constants";
import {
  buildActiveMinerColumns,
  configurableMinerColumns,
  createDefaultMinerTableColumnPreferences,
  normalizeMinerTableColumnPreferences,
} from "./minerTableColumnPreferences";

describe("minerTableColumnPreferences", () => {
  it("places pool configuration immediately after issues by default", () => {
    const columns = buildActiveMinerColumns(createDefaultMinerTableColumnPreferences());
    expect(columns[columns.indexOf(minerCols.issues) + 1]).toBe(minerCols.poolConfiguration);
  });
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
    const preferences = {
      columns: configurableMinerColumns.map((columnId) => ({
        id: columnId,
        visible: columnId !== minerCols.macAddress,
      })),
    };

    expect(buildActiveMinerColumns(preferences)).toEqual([
      minerCols.name,
      ...configurableMinerColumns.filter((columnId) => columnId !== minerCols.macAddress),
    ]);
  });

  it("lets allocation visibility and position follow saved column choices", () => {
    const preferences = normalizeMinerTableColumnPreferences({
      columns: [
        { id: minerCols.status, visible: true },
        { id: minerCols.assignedAccount, visible: false },
      ],
    });
    expect(buildActiveMinerColumns(preferences)).not.toContain(minerCols.assignedAccount);
    const visible = normalizeMinerTableColumnPreferences({
      columns: [
        { id: minerCols.status, visible: true },
        { id: minerCols.assignedAccount, visible: true },
      ],
    });
    expect(buildActiveMinerColumns(visible).slice(0, 3)).toEqual([
      minerCols.name,
      minerCols.status,
      minerCols.assignedAccount,
    ]);
  });
});
