import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import BoardIdentifyDialog from "./BoardIdentifyDialog";
import MiningActionsMenu from "./MiningActionsMenu";
import {
  BoardIdentifyMode,
  GetBoardIdentifyOptionsResponseSchema,
} from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { EntityKind, InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

const mocks = vi.hoisted(() => ({
  options: vi.fn(),
  identifyOptions: vi.fn(),
  identify: vi.fn(),
  results: vi.fn(),
  permissions: [] as string[],
}));
vi.mock("@/protoFleet/api/clients", () => ({
  containersClient: {
    getModuleMiningOptions: mocks.options,
    getBoardIdentifyOptions: mocks.identifyOptions,
    setBoardIdentify: mocks.identify,
    getEquipmentCommandResult: mocks.results,
  },
}));
vi.mock("@/protoFleet/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoFleet/store")>()),
  usePermissions: () => mocks.permissions,
}));
vi.mock("@/shared/components/Popover/usePopoverPosition", () => ({
  default: () => ({ popoverRef: { current: null }, popoverStyle: {}, popoverAnimation: "" }),
}));

const board = (id: bigint, slot: number) => ({
  hashboardId: id,
  installationId: id * 10n,
  moduleId: 11n,
  deviceIdentifier: "north",
  containerName: "North CT1",
  tank: 1,
  module: 2,
  slot,
  serialNumber: `SN-${slot}`,
});
const recordedAt = Date.UTC(2026, 10, 13, 15, 0);
const now = Date.UTC(2026, 10, 13, 15, 5);
const clock = (ms: number) => new Date(ms).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
const options = (overrides = {}) =>
  create(GetBoardIdentifyOptionsResponseSchema, {
    boards: [board(101n, 1), board(102n, 2), board(103n, 3)],
    canIdentify: true,
    ...overrides,
  });

afterEach(() => {
  vi.restoreAllMocks();
});

beforeEach(() => {
  vi.clearAllMocks();
  mocks.permissions = ["miner:blink_led"];
  mocks.options.mockResolvedValue({ canStart: true, canStop: true, unavailableReason: "" });
  mocks.identifyOptions.mockResolvedValue(options());
  mocks.identify.mockResolvedValue({
    commands: [101n, 102n, 103n].map((id) => ({
      hashboardId: id,
      deviceIdentifier: "north",
      batchIdentifier: `batch-${id}`,
    })),
  });
  mocks.results.mockImplementation(async ({ batchIdentifier }: { batchIdentifier: string }) => ({
    result:
      batchIdentifier === "batch-102"
        ? {
            status: "finished",
            successCount: 0,
            failureCount: 1,
            deviceResults: [{ errorMessage: "Board was replaced; refresh before retrying." }],
          }
        : {
            status: "finished",
            successCount: 1,
            failureCount: 0,
            // Board 103's row carries no timestamp, so its end time counts from now.
            deviceResults: batchIdentifier === "batch-101" ? [{ updatedAt: timestampFromMs(recordedAt) }] : [],
          },
  }));
});

it("confirms the expanded boards, dispatches the exact installations, and reports each board's outcome", async () => {
  vi.spyOn(Date, "now").mockReturnValue(now);
  render(<BoardIdentifyDialog moduleIds={[11n]} blink onClose={vi.fn()} />);
  const list = await screen.findByRole("list", { name: "Affected boards" });
  expect(screen.getByTestId("board-identify-dialog")).toHaveTextContent(
    "The identification LED on each listed board will blink for about 15 minutes.",
  );
  expect(mocks.identifyOptions).toHaveBeenCalledExactlyOnceWith(
    { moduleIds: [11n], hashboardIds: [] },
    expect.anything(),
  );
  expect(within(list).getAllByRole("listitem")).toHaveLength(3);
  expect(list).toHaveTextContent("North CT1 · Tank 1 · Module 2 · Board 2");
  expect(mocks.identify).not.toHaveBeenCalled();

  fireEvent.click(screen.getByRole("button", { name: "Blink LEDs" }));
  const items = within(list).getAllByRole("listitem");
  await waitFor(() => expect(items[2]).toHaveTextContent(`Blinking until about ${clock(now + 15 * 60_000)}`));
  expect(items[0]).toHaveTextContent(`Blinking until about ${clock(recordedAt + 15 * 60_000)}`);
  expect(mocks.identify).toHaveBeenCalledExactlyOnceWith(
    {
      boards: [
        { hashboardId: 101n, installationId: 1010n },
        { hashboardId: 102n, installationId: 1020n },
        { hashboardId: 103n, installationId: 1030n },
      ],
      mode: BoardIdentifyMode.BLINK,
    },
    expect.anything(),
  );
  expect(items[1]).toHaveTextContent("Board was replaced; refresh before retrying.");
  expect(screen.getByRole("button", { name: "Close" })).toBeEnabled();
});

it("moves keyboard focus into the dialog and keeps Tab inside it", async () => {
  render(
    <>
      <button>Background</button>
      <BoardIdentifyDialog moduleIds={[11n]} blink onClose={vi.fn()} />
    </>,
  );
  await screen.findByRole("list", { name: "Affected boards" });
  const dialog = screen.getByRole("dialog");
  expect(dialog).toContainElement(document.activeElement as HTMLElement);
  const confirm = screen.getByRole("button", { name: "Blink LEDs" });
  confirm.focus();
  fireEvent.keyDown(document, { key: "Tab" });
  expect(dialog).toContainElement(document.activeElement as HTMLElement);
  expect(screen.getByRole("button", { name: "Background" })).not.toHaveFocus();
});

it("ignores Escape while the request is sending, then closes once outcomes show", async () => {
  let respond: (value: unknown) => void = () => {};
  mocks.identify.mockReturnValue(new Promise((resolve) => (respond = resolve)));
  const onClose = vi.fn();
  render(<BoardIdentifyDialog moduleIds={[11n]} blink onClose={onClose} />);
  fireEvent.click(await screen.findByRole("button", { name: "Blink LEDs" }));
  expect(screen.getByRole("button", { name: "Sending…" })).toBeDisabled();
  fireEvent.keyDown(document, { key: "Escape" });
  expect(onClose).not.toHaveBeenCalled();

  respond({ commands: [{ hashboardId: 101n, deviceIdentifier: "north", batchIdentifier: "batch-101" }] });
  await screen.findByRole("button", { name: "Close" });
  fireEvent.keyDown(document, { key: "Escape" });
  expect(onClose).toHaveBeenCalledOnce();
});

it("blocks dispatch and explains why when the boards cannot be identified", async () => {
  mocks.identifyOptions.mockResolvedValue(
    options({ canIdentify: false, unavailableReason: "Board identity is unavailable or stale." }),
  );
  render(<BoardIdentifyDialog hashboardIds={[101n]} blink={false} onClose={vi.fn()} />);
  expect(await screen.findByRole("alert")).toHaveTextContent("Board identity is unavailable or stale.");
  const confirm = screen.getByRole("button", { name: "Stop blinking LEDs" });
  expect(confirm).toBeDisabled();
  fireEvent.click(confirm);
  expect(mocks.identify).not.toHaveBeenCalled();
});

it("keeps the confirmation open with the server's reason when the boards changed after confirmation", async () => {
  mocks.identify.mockRejectedValue(
    new Error("A selected board moved or was replaced after you confirmed it. Review the boards and try again."),
  );
  render(<BoardIdentifyDialog moduleIds={[11n]} blink={false} onClose={vi.fn()} />);
  fireEvent.click(await screen.findByRole("button", { name: "Stop blinking LEDs" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("moved or was replaced");
  expect(mocks.identify).toHaveBeenCalledWith(
    expect.objectContaining({ mode: BoardIdentifyMode.OFF }),
    expect.anything(),
  );
  expect(mocks.results).not.toHaveBeenCalled();
  expect(screen.queryByRole("status")).not.toBeInTheDocument();
});

it("reloads the boards after a rejected submit so a retry sends the current installations", async () => {
  mocks.identifyOptions
    .mockResolvedValueOnce(options())
    .mockResolvedValueOnce(
      options({ boards: [board(101n, 1), { ...board(102n, 2), installationId: 2020n }, board(103n, 3)] }),
    );
  mocks.identify.mockRejectedValueOnce(
    new Error("A selected board moved or was replaced after you confirmed it. Review the boards and try again."),
  );
  render(<BoardIdentifyDialog moduleIds={[11n]} blink onClose={vi.fn()} />);
  fireEvent.click(await screen.findByRole("button", { name: "Blink LEDs" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("moved or was replaced");
  await waitFor(() => expect(mocks.identifyOptions).toHaveBeenCalledTimes(2));
  const retry = await screen.findByRole("button", { name: "Blink LEDs" });
  await waitFor(() => expect(retry).toBeEnabled());
  expect(screen.getByRole("alert")).toHaveTextContent("moved or was replaced");

  fireEvent.click(retry);
  await screen.findByRole("button", { name: "Close" });
  expect(mocks.identify).toHaveBeenLastCalledWith(
    expect.objectContaining({
      boards: [
        { hashboardId: 101n, installationId: 1010n },
        { hashboardId: 102n, installationId: 2020n },
        { hashboardId: 103n, installationId: 1030n },
      ],
    }),
    expect.anything(),
  );
});

it("offers module board identification only with locator permission", async () => {
  const modules = [create(InventoryItemSchema, { ref: { kind: EntityKind.MODULE, id: 11n }, name: "North T1 M2" })];
  const { unmount } = render(<MiningActionsMenu kind="module" items={modules} />);
  fireEvent.click(screen.getByRole("button", { name: "Actions for North T1 M2" }));
  fireEvent.click(await screen.findByRole("button", { name: "Stop blinking LEDs" }));
  expect(await screen.findByTestId("board-identify-dialog")).toHaveTextContent("Stop blinking LEDs on 3 boards?");
  expect(mocks.identifyOptions).toHaveBeenCalledWith({ moduleIds: [11n], hashboardIds: [] }, expect.anything());
  unmount();

  mocks.permissions = [];
  render(<MiningActionsMenu kind="module" items={modules} />);
  fireEvent.click(screen.getByRole("button", { name: "Actions for North T1 M2" }));
  await waitFor(() => expect(mocks.options).toHaveBeenCalled());
  expect(screen.queryByRole("button", { name: "Blink LEDs" })).not.toBeInTheDocument();
});
