import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { HashboardCell } from "./HashboardCell";
import { MinerError } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { HashboardSlotSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

it.each(["empty", "unknown", "occupied"])("leaves issues blank for %s slots without causes", (occupancy) => {
  const slot = create(HashboardSlotSchema, {
    position: 1,
    occupancy,
    board: occupancy === "occupied" ? { health: { errors: [], warnings: [] } } : undefined,
  });
  const { container } = render(<HashboardCell slot={slot} column="issues" />);
  expect(container).toBeEmptyDOMElement();
});

it("uses miner issue summaries and shows warning and stack details without low-level diagnostics on click", () => {
  const slot = create(HashboardSlotSchema, {
    position: 2,
    occupancy: "occupied",
    board: {
      serial: "CT1-379e6466-0001",
      macAddress: "aa:bb",
      observedAt: timestampFromDate(new Date()),
      issues: [
        {
          component: "hashboard",
          componentId: "aa:bb",
          cause: "HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE",
          severity: "warning",
          canonicalError: MinerError.HASHBOARD_OVER_TEMPERATURE,
          observedAt: timestampFromDate(new Date()),
        },
      ],
      hashrailOk: false,
      refclksOk: true,
      health: { warnings: [{ cause: "HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE", stack: 2 }] },
    },
  });
  render(<HashboardCell slot={slot} column="issues" />);
  fireEvent.click(screen.getByRole("button", { name: "Hashboard 2 - CT1-379e6466-0001 issues: Hashboard 2 failure" }));
  expect(screen.getByTestId("module-issues")).toHaveTextContent("Over temperature (stack 2)");
  expect(screen.getByTestId("module-issues")).toHaveTextContent("Warning");
  expect(screen.getByTestId("module-issues")).not.toHaveTextContent("Hashrail");
  expect(screen.getByTestId("module-issues")).not.toHaveTextContent("Reference clocks");
});

it.each([
  ["power", { powerW: 0 }, "0.0 kW"],
  ["power", {}, "—"],
  ["efficiency", { efficiencyJh: 10e-12 }, "10.0 J/TH"],
  ["efficiency", {}, "—"],
])("shows stored nullable board %s", (column, board, expected) => {
  const slot = create(HashboardSlotSchema, { position: 1, occupancy: "occupied", board });
  render(<HashboardCell slot={slot} column={column} />);
  expect(screen.getByText(expected)).toBeInTheDocument();
});

it("keeps isolation separate from the canonical operating state", () => {
  const slot = create(HashboardSlotSchema, {
    position: 1,
    occupancy: "occupied",
    board: { state: "active", isolated: true },
  });
  render(<HashboardCell slot={slot} column="status" />);
  expect(screen.getByText("Hashing")).toBeInTheDocument();
  expect(screen.getByTestId("module-status-indicator")).toHaveAttribute("data-status", "normal");
  expect(screen.getByText(/isolated/i)).toBeInTheDocument();
});
