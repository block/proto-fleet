import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { MinerScopeChanges } from "./channelSettingsChangesUtils";
import MinerScopeChangesModal from "./MinerScopeChangesModal";

afterEach(cleanup);

const changesFor = (miners: MinerScopeChanges["miners"]): MinerScopeChanges => ({
  miners,
  originalCount: miners.filter((miner) => miner.original).length,
  targetCount: miners.filter((miner) => miner.target).length,
  addedCount: miners.filter((miner) => !miner.original && miner.target).length,
  removedCount: miners.filter((miner) => miner.original && !miner.target).length,
});
const minerRow = (identifier: string) => {
  const table = within(screen.getByRole("table", { name: "Miner changes" }));
  return within(table.getByText(identifier, { exact: true }).closest("tr")!);
};
const expectMembership = (identifier: string, original: string, target: string) => {
  const cells = minerRow(identifier).getAllByRole("cell");
  // The first column can be a regular cell or a row header; inspect the two membership columns.
  expect(cells.slice(-2).map((cell) => cell.textContent)).toEqual([original, target]);
};

const mixedChanges = changesFor([
  { identifier: "miner-removed", name: "Rig", original: true, target: false },
  { identifier: "miner-retained", name: "Rig", original: true, target: true },
  { identifier: "miner-added", name: "New rig", original: false, target: true },
]);

describe("miner scope changes details", () => {
  it("shows complete original and target membership, including retained miners and duplicate names", () => {
    render(<MinerScopeChangesModal changes={mixedChanges} onClose={vi.fn()} />);
    const table = within(screen.getByRole("table", { name: "Miner changes" }));
    expect(table.getAllByRole("columnheader").map((header) => header.textContent)).toEqual([
      "Miner",
      "Original",
      "Target",
    ]);
    expect(table.getAllByRole("row")).toHaveLength(4);
    expect(minerRow("miner-removed").getByText("Rig", { exact: true })).toBeInTheDocument();
    expect(minerRow("miner-retained").getByText("Rig", { exact: true })).toBeInTheDocument();
    expectMembership("miner-removed", "Included", "Not included");
    expectMembership("miner-retained", "Included", "Included");
    expectMembership("miner-added", "Not included", "Included");
  });

  it("searches miner names and identifiers without changing original or target membership", () => {
    render(<MinerScopeChangesModal changes={mixedChanges} onClose={vi.fn()} />);
    const search = screen.getByRole("textbox", { name: "Search miners" });
    fireEvent.change(search, { target: { value: "NEW RIG" } });
    expectMembership("miner-added", "Not included", "Included");
    expect(screen.queryByText("miner-removed", { exact: true })).not.toBeInTheDocument();
    fireEvent.change(search, { target: { value: "miner-removed" } });
    expectMembership("miner-removed", "Included", "Not included");
    expect(screen.queryByText("miner-added", { exact: true })).not.toBeInTheDocument();
    fireEvent.change(search, { target: { value: "no matching miner" } });
    expect(screen.getByText("No miners match your search.")).toBeInTheDocument();
    fireEvent.change(search, { target: { value: "" } });
    expect(within(screen.getByRole("table", { name: "Miner changes" })).getAllByRole("row")).toHaveLength(4);
  });

  it("paginates the full list and resets to the first page when the search changes", () => {
    const miners = Array.from({ length: 101 }, (_, index) => ({
      identifier: `miner-${String(index + 1).padStart(3, "0")}`,
      name: `Rig ${index + 1}`,
      original: index < 100,
      target: index > 0,
    }));
    render(<MinerScopeChangesModal changes={changesFor(miners)} onClose={vi.fn()} />);
    const previous = screen.getByRole("button", { name: "Previous" });
    const next = screen.getByRole("button", { name: "Next" });
    expect(screen.getByText("1–50 of 101 miners")).toBeInTheDocument();
    expect(previous).toBeDisabled();
    expectMembership("miner-001", "Included", "Not included");
    expect(screen.queryByText("miner-051", { exact: true })).not.toBeInTheDocument();

    fireEvent.click(next);
    expect(screen.getByText("51–100 of 101 miners")).toBeInTheDocument();
    expectMembership("miner-051", "Included", "Included");
    expect(screen.queryByText("miner-001", { exact: true })).not.toBeInTheDocument();
    fireEvent.click(next);
    expect(screen.getByText("101–101 of 101 miners")).toBeInTheDocument();
    expectMembership("miner-101", "Not included", "Included");
    expect(next).toBeDisabled();
    fireEvent.click(previous);
    expect(screen.getByText("51–100 of 101 miners")).toBeInTheDocument();

    fireEvent.change(screen.getByRole("textbox", { name: "Search miners" }), {
      target: { value: "miner-001" },
    });
    expectMembership("miner-001", "Included", "Not included");
    expect(screen.queryByText("No miners match your search.")).not.toBeInTheDocument();
    fireEvent.change(screen.getByRole("textbox", { name: "Search miners" }), { target: { value: "" } });
    expect(screen.getByText("1–50 of 101 miners")).toBeInTheDocument();
  });

  it.each([
    { original: false, target: true, originalLabel: "Not included", targetLabel: "Included" },
    { original: true, target: false, originalLabel: "Included", targetLabel: "Not included" },
  ])(
    "preserves the full list when one selection is empty ($original → $target)",
    ({ original, target, originalLabel, targetLabel }) => {
      render(
        <MinerScopeChangesModal
          changes={changesFor([{ identifier: "only-miner", name: "Only rig", original, target }])}
          onClose={vi.fn()}
        />,
      );
      expectMembership("only-miner", originalLabel, targetLabel);
    },
  );

  it.each(["Done", "Escape"])("closes via %s without changing the captured selections", (action) => {
    const onClose = vi.fn();
    const changes = structuredClone(mixedChanges);
    render(<MinerScopeChangesModal changes={changes} onClose={onClose} />);
    if (action === "Done") fireEvent.click(screen.getByRole("button", { name: "Done" }));
    else fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalledOnce();
    expect(changes).toEqual(mixedChanges);
  });
});
