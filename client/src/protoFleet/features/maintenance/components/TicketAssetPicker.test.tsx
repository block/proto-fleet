import { render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";
import TicketAssetPicker from "./TicketAssetPicker";
import { TicketAssetSchema, TicketCategory } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";

describe("TicketAssetPicker", () => {
  it("allows a module and its boards while preventing another parent machine", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    const assets = [
      create(TicketAssetSchema, { kind: TicketCategory.MODULE, id: 1n, siteId: 8n, label: "Module 1" }),
      create(TicketAssetSchema, {
        kind: TicketCategory.HASHBOARD,
        id: 11n,
        moduleId: 1n,
        siteId: 8n,
        label: "Board 11",
      }),
      create(TicketAssetSchema, {
        kind: TicketCategory.HASHBOARD,
        id: 21n,
        moduleId: 2n,
        siteId: 8n,
        label: "Board 21",
      }),
      create(TicketAssetSchema, { kind: TicketCategory.MINER, id: 3n, siteId: 8n, label: "Miner 3" }),
    ];
    render(
      <TicketAssetPicker
        singleParent
        load={async () => ({ assets, nextPageToken: "" })}
        onSelect={onSelect}
        onDismiss={vi.fn()}
      />,
    );
    const dialog = within(screen.getByRole("dialog", { name: "Select affected assets" }));
    const checkboxes = await dialog.findAllByRole("checkbox");
    await user.click(checkboxes[0]);
    expect(checkboxes[1]).toBeEnabled();
    expect(checkboxes[2]).toBeDisabled();
    expect(checkboxes[3]).toBeDisabled();
    await user.click(checkboxes[1]);
    await user.click(dialog.getByRole("button", { name: "Select assets" }));
    expect(onSelect).toHaveBeenCalledWith([assets[0], assets[1]]);
  });
});

it("keeps selections and parent restrictions across pages and resets pagination when searching", async () => {
  const user = userEvent.setup();
  const onSelect = vi.fn();
  const module = create(TicketAssetSchema, { kind: TicketCategory.MODULE, id: 1n, label: "Module 1" });
  const board = create(TicketAssetSchema, { kind: TicketCategory.HASHBOARD, id: 11n, moduleId: 1n, label: "Board 11" });
  const other = create(TicketAssetSchema, { kind: TicketCategory.MINER, id: 3n, label: "Miner 3" });
  const load = vi.fn(async (search: string, _signal: AbortSignal, pageToken: string) =>
    search
      ? { assets: [board], nextPageToken: "" }
      : pageToken
        ? { assets: [board, other], nextPageToken: "" }
        : { assets: [module], nextPageToken: "boards" },
  );
  render(<TicketAssetPicker singleParent load={load} onSelect={onSelect} onDismiss={vi.fn()} />);
  await user.click(await screen.findByRole("checkbox"));
  await user.click(screen.getByRole("button", { name: "Next page" }));
  const boxes = await screen.findAllByRole("checkbox");
  expect(boxes).toHaveLength(2);
  expect(boxes[0]).toBeEnabled();
  expect(boxes[1]).toBeDisabled();
  await user.click(boxes[0]);
  expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Previous page" }));
  expect(await screen.findByRole("checkbox")).toBeChecked();
  await user.click(screen.getByRole("button", { name: "Next page" }));
  await screen.findByText(/Board 11/);
  await user.type(screen.getByRole("textbox", { name: "Search assets" }), "board");
  await waitFor(() => expect(load).toHaveBeenLastCalledWith("board", expect.any(AbortSignal), ""));
  expect(await screen.findByRole("checkbox")).toBeChecked();
  expect(screen.getByRole("button", { name: "Previous page" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Select assets" }));
  expect(onSelect).toHaveBeenCalledWith([module, board]);
});
