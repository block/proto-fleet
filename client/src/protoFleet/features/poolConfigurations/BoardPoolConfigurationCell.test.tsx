import { render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { PoolAssignmentsContext } from "./assignmentContext";
import { usePoolAssignmentProgress } from "./assignmentProgress";
import { BoardPoolConfigurationCell } from "./BoardPoolConfigurationCell";
import { EquipmentAssignmentSchema } from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";

vi.mock("@/protoFleet/features/hostedAccounts/AccountScope", () => ({ useAccountScope: () => undefined }));

beforeEach(() => usePoolAssignmentProgress.setState({ jobs: [] }));

it.each([
  ["verified", "Mixed configurations"],
  ["cleared", "—"],
  ["failed", "Needs attention"],
  ["uncertain", "Needs attention"],
])("aggregates a cleared board with a %s board as %s", (state, expected) => {
  const boards = [
    create(EquipmentAssignmentSchema, { moduleId: 1n, installationId: 1n, state: "cleared" }),
    create(EquipmentAssignmentSchema, {
      moduleId: 1n,
      installationId: 2n,
      configurationId: state === "cleared" ? "" : "configured",
      state,
    }),
  ];
  render(
    <PoolAssignmentsContext.Provider
      value={{
        boards,
        byIdentifier: new Map(),
        byId: new Map(),
        readable: true,
        loading: false,
        error: null,
        refresh: vi.fn(),
      }}
    >
      <BoardPoolConfigurationCell moduleId={1n} />
    </PoolAssignmentsContext.Provider>,
  );
  expect(screen.getByText(expected)).toBeVisible();
});
