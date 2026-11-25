import { expect, test } from "vitest";
import { matchesModuleFilters, parseModuleListState, writeModuleFilters } from "./moduleListFilters";
import { buildModuleRows } from "./moduleListModel";
import { tanksFixture } from "@/protoOS/features/tanks/Tanks.fixtures";

test("combines categories with AND, values with OR, and uses canonical tank IDs", () => {
  const { topology, telemetry } = tanksFixture(2, 12);
  const rows = buildModuleRows(topology, telemetry);
  const params = new URLSearchParams();
  params.append("status", "healthy");
  params.append("status", "attention");
  params.append("tank", [...topology.tanks.keys()][1]);
  const { filters } = parseModuleListState(params);
  const matching = rows.filter((row) => matchesModuleFilters(row, filters));
  expect(matching).toHaveLength(11);
  expect(matching.every((row) => row.name.startsWith("T2-"))).toBe(true);
});

test("filter writes preserve unrelated parameters and invalid sort values use defaults", () => {
  const params = new URLSearchParams("other=value&sort=invalid&direction=wat&status=alien");
  const { filters, sort } = parseModuleListState(params);
  expect(sort).toEqual({ field: "name", direction: "asc" });
  expect(filters.dropdownFilters.status).toEqual([]);
  filters.dropdownFilters.issues = ["present"];
  const next = writeModuleFilters(params, filters);
  expect(next.get("other")).toBe("value");
  expect(next.get("issues")).toBe("present");
});
