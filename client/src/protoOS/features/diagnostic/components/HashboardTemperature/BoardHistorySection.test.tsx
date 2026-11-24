import { render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import BoardHistorySection from "./BoardHistorySection";
import type { MinerAdapter } from "@/protoOS/api/adapters/types";
import type { BoardEventPage, RecoveryEvidenceSummary } from "@/shared/features/boardHistory/types";

const mocks = vi.hoisted(() => ({
  handleAuthErrors: vi.fn(),
  downloadBlob: vi.fn(),
  adapter: undefined as MinerAdapter | undefined,
}));
vi.mock("@/shared/utils/utility", async (original) => ({
  ...(await original<typeof import("@/shared/utils/utility")>()),
  downloadBlob: mocks.downloadBlob,
}));
vi.mock("@/protoOS/store/hooks/useAuth", () => ({
  useAuthErrors: () => ({ handleAuthErrors: mocks.handleAuthErrors }),
}));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter: mocks.adapter, isFleetHosted: false }),
}));

const MAC = "02:00:00:00:00:0a";
const page = (overrides: Partial<BoardEventPage> = {}): BoardEventPage => ({
  events: [
    {
      id: "1",
      kind: "error",
      time: "2026-11-15T10:00:00Z",
      lastSeen: "2026-11-15T10:18:00Z",
      samples: 36,
      cause: "HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE",
    },
    { id: "2", kind: "stackDropout", time: "2026-11-15T10:05:00Z", stack: 2 },
  ],
  window: { start: "2026-11-15T00:00:00Z", end: "2026-11-16T00:00:00Z" },
  fetchedAt: "2026-11-16T00:00:00Z",
  ...overrides,
});
const summary: RecoveryEvidenceSummary = {
  id: "evidence-1",
  episodeId: "episode-3",
  trigger: "power_recovery",
  causes: ["HASHBOARD_ERROR_CAUSE_BOARD_UNDER_VOLTAGE"],
  capturedAt: "2026-11-14T08:00:00Z",
  capturedBy: "ct1-a",
  truncated: true,
};

describe("Board history section", () => {
  beforeEach(() => {
    mocks.handleAuthErrors.mockImplementation(({ onSuccess }) => onSuccess());
  });
  afterEach(() => {
    vi.restoreAllMocks();
    vi.clearAllMocks();
    mocks.adapter = undefined;
  });

  it("renders nothing when the adapter has no board history", () => {
    mocks.adapter = {} as MinerAdapter;
    const { container } = render(<BoardHistorySection mac={MAC} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows past events for the board's window, and pages to the window before it", async () => {
    const getBoardEvents = vi.fn().mockResolvedValue(page());
    mocks.adapter = { getBoardEvents } as unknown as MinerAdapter;
    render(<BoardHistorySection mac={MAC} />);

    expect(await screen.findByText("Error: Board over temperature")).toBeInTheDocument();
    expect(screen.getByText("Stack 2 (firmware index) dropped out")).toBeInTheDocument();
    expect(screen.getByText(/not its current state/)).toBeInTheDocument();
    const [identifier, first] = getBoardEvents.mock.calls[0];
    expect(identifier).toBe(MAC);
    expect(Date.parse(first.end) - Date.parse(first.start)).toBe(24 * 60 * 60 * 1000);
    expect(screen.getByTestId("board-history-later")).toBeDisabled();

    await userEvent.click(screen.getByTestId("board-history-earlier"));

    await waitFor(() => expect(getBoardEvents).toHaveBeenCalledTimes(2));
    // The earlier window ends where the controller's effective window started.
    expect(getBoardEvents.mock.calls[1][1].end).toBe("2026-11-15T00:00:00.000Z");
    expect(screen.getByTestId("board-history-later")).toBeEnabled();
  });

  it("pages back to the live window through the windows it passed, and resets on a new range", async () => {
    type Query = { start: string; end: string };
    const getBoardEvents = vi.fn((_mac: string, query: Query) => Promise.resolve(page({ window: query })));
    mocks.adapter = { getBoardEvents } as unknown as MinerAdapter;
    const queries = () => getBoardEvents.mock.calls.map(([, query]) => query);
    const click = async (testId: string, reads: number) => {
      await userEvent.click(screen.getByTestId(testId));
      await waitFor(() => expect(getBoardEvents).toHaveBeenCalledTimes(reads));
    };
    render(<BoardHistorySection mac={MAC} />);
    await screen.findByText("Error: Board over temperature");

    await click("board-history-earlier", 2);
    await waitFor(() => expect(screen.getByTestId("board-history-window")).toBeInTheDocument());
    await click("board-history-earlier", 3);
    expect(queries()[2].end).toBe(queries()[1].start);

    await click("board-history-later", 4);
    expect(queries()[3]).toEqual(queries()[1]);
    expect(screen.getByTestId("board-history-later")).toBeEnabled();

    // The last step back reads up to now, not up to when the first read was made.
    await click("board-history-later", 5);
    expect(Date.parse(queries()[4].end)).toBeGreaterThanOrEqual(Date.parse(queries()[0].end));
    expect(screen.getByTestId("board-history-later")).toBeDisabled();

    await click("board-history-earlier", 6);
    // The option list opens only once the Select can measure its trigger.
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
    await userEvent.click(screen.getByRole("button", { name: "Window" }));
    await userEvent.click(screen.getByRole("option", { name: "7 days" }));
    await waitFor(() => expect(getBoardEvents).toHaveBeenCalledTimes(7));
    const week = queries()[6];
    expect(Date.parse(week.end) - Date.parse(week.start)).toBe(7 * 24 * 60 * 60 * 1000);
    expect(Date.parse(week.end)).toBeGreaterThanOrEqual(Date.parse(queries()[0].end));
    expect(screen.getByTestId("board-history-later")).toBeDisabled();
  });

  it("starts over on the live window when the board changes", async () => {
    const other = "02:00:00:00:00:0b";
    const getBoardEvents = vi.fn((mac: string) =>
      Promise.resolve(
        mac === MAC
          ? page()
          : page({
              events: [
                {
                  id: "9",
                  kind: "warning",
                  time: "2026-11-15T11:00:00Z",
                  cause: "HASHBOARD_ERROR_CAUSE_BOARD_UNDER_VOLTAGE",
                },
              ],
            }),
      ),
    );
    mocks.adapter = { getBoardEvents } as unknown as MinerAdapter;
    const { rerender } = render(<BoardHistorySection mac={MAC} />);
    await screen.findByText("Error: Board over temperature");
    await userEvent.click(screen.getByTestId("board-history-earlier"));
    await waitFor(() => expect(screen.getByTestId("board-history-later")).toBeEnabled());

    rerender(<BoardHistorySection mac={other} />);

    expect(await screen.findByText("Warning: Board under voltage")).toBeInTheDocument();
    expect(screen.queryByText("Error: Board over temperature")).not.toBeInTheDocument();
    expect(screen.getByTestId("board-history-later")).toBeDisabled();
    const calls = getBoardEvents.mock.calls as unknown as [string, { start: string; end: string }][];
    const [identifier, query] = calls[calls.length - 1];
    expect(identifier).toBe(other);
    expect(Date.parse(query.end) - Date.parse(query.start)).toBe(24 * 60 * 60 * 1000);
  });

  it("says when history can't be read", async () => {
    mocks.adapter = {
      getBoardEvents: vi.fn().mockRejectedValue(new Error("history store offline")),
    } as unknown as MinerAdapter;
    render(<BoardHistorySection mac={MAC} />);
    expect(await screen.findByTestId("board-history-unavailable")).toHaveTextContent("history store offline");
  });

  it("opens retained evidence and a snapshot's coverage on request", async () => {
    const listRecoveryEvidence = vi.fn().mockResolvedValue([summary]);
    const json = JSON.stringify({
      sources: { health: { status: "ok", count: 1 }, asic_frames: { status: "error", detail: "timed out" } },
      omitted: ["asic_frames"],
      events: { stack_dropouts: [{ stack: 1, cleared_at: "2026-11-14T08:04:00Z" }] },
    });
    const getRecoveryEvidence = vi.fn().mockResolvedValue({ summary, json });
    mocks.adapter = {
      getBoardEvents: vi.fn().mockResolvedValue(page({ events: [] })),
      listRecoveryEvidence,
      getRecoveryEvidence,
    } as unknown as MinerAdapter;
    render(<BoardHistorySection mac={MAC} />);
    expect(await screen.findByText("No events recorded in this window")).toBeInTheDocument();
    expect(listRecoveryEvidence).not.toHaveBeenCalled();

    await userEvent.click(screen.getByTestId("board-history-evidence"));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(await within(dialog).findByText("Power recovery"));

    expect(await within(dialog).findByTestId("recovery-evidence-truncated")).toHaveTextContent("ASIC frames");
    expect(within(dialog).getByTestId("recovery-evidence-sources")).toHaveTextContent("Failed: timed out");
    expect(within(dialog).getByText("Stack 1 (firmware index)")).toBeInTheDocument();
    expect(listRecoveryEvidence).toHaveBeenCalledWith(MAC, expect.anything());
    expect(getRecoveryEvidence).toHaveBeenCalledWith("evidence-1", expect.anything());

    // The download is the stored document, byte for byte.
    await userEvent.click(within(dialog).getByRole("button", { name: "Download record (JSON)" }));
    const [blob, filename] = mocks.downloadBlob.mock.calls[0] as [Blob, string];
    expect(await blob.text()).toBe(json);
    expect(filename).toMatch(/^recovery-evidence-02000000000a-evidence-1.*\.json$/);
    expect(filename).not.toContain(":");
  });
});
