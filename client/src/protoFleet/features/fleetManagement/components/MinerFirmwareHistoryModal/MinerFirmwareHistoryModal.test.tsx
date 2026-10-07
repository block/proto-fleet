import { MemoryRouter, useLocation } from "react-router-dom";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import MinerFirmwareHistoryModal, { MinerFirmwareHistoryModalView } from "./MinerFirmwareHistoryModal";
import { completedHistoryEntry } from "./MinerFirmwareHistoryModal.fixtures";
import {
  MinerFirmwareHistoryEntrySchema,
  RolloutDevicePhase,
  RolloutStatus,
} from "@/protoFleet/api/generated/rollout/v1/rollout_pb";
import { type MinerFirmwareHistoryState, useMinerFirmwareHistory } from "@/protoFleet/api/useMinerFirmwareHistory";

vi.mock("@/protoFleet/api/useMinerFirmwareHistory", () => ({ useMinerFirmwareHistory: vi.fn() }));

const historyState = (overrides: Partial<MinerFirmwareHistoryState> = {}): MinerFirmwareHistoryState => ({
  entries: [completedHistoryEntry],
  canRead: true,
  hasLoaded: true,
  isLoading: false,
  isLoadingMore: false,
  hasMore: false,
  error: null,
  refresh: vi.fn(),
  loadMore: vi.fn(),
  retry: vi.fn(),
  ...overrides,
});
const propsFor = (history = historyState()) => ({
  deviceIdentifier: "miner-123",
  minerName: "Rack 1 · Miner 2",
  history,
  onClose: vi.fn(),
  onViewUpdate: vi.fn(),
});

describe("MinerFirmwareHistoryModal", () => {
  it("shows the miner's verified outcome and date independently of the overall rollout", () => {
    render(<MinerFirmwareHistoryModalView {...propsFor()} />);
    expect(screen.getByText("Updated")).toBeInTheDocument();
    expect(screen.queryByText("Failed")).not.toBeInTheDocument();
    expect(screen.getByText("Verified at")).toBeInTheDocument();
    const row = screen.getByText("1.4.3").closest("tr")!;
    expect(row.querySelectorAll("time")).toHaveLength(1);
    expect(row.querySelector("time")).toHaveAttribute("dateTime", "2026-09-26T10:08:00.000Z");
    expect(within(row).queryByText("Proto Rig")).not.toBeInTheDocument();
    expect(
      screen.getByText(
        "History includes only release channel updates, with attempt counts reset on retry and entries removed when their channel is deleted.",
      ),
    ).toBeInTheDocument();
  });

  it.each([RolloutDevicePhase.QUEUED, RolloutDevicePhase.IN_PROGRESS, RolloutDevicePhase.RETRYING])(
    "labels canceled unfinished miner phase %s as Canceled",
    (phase) => {
      const entry = create(MinerFirmwareHistoryEntrySchema, {
        ...completedHistoryEntry,
        phase,
        rolloutStatus: RolloutStatus.CANCELED,
        verifiedAt: undefined,
      });
      render(<MinerFirmwareHistoryModalView {...propsFor(historyState({ entries: [entry] }))} />);
      expect(screen.getByText("Canceled")).toBeInTheDocument();
      expect(screen.getByText("Any update command already sent may still finish.")).toBeInTheDocument();
      expect(screen.queryByText(/^(Queued|Updating|Retrying)$/)).not.toBeInTheDocument();
      const row = screen.getByText("1.4.3").closest("tr")!;
      expect(within(row).getByText("—")).toBeInTheDocument();
      expect(row.querySelector("time")).not.toBeInTheDocument();
    },
  );

  it("retains settled miner outcomes in a canceled rollout and handles unknown phases", () => {
    const phases = [
      RolloutDevicePhase.DONE,
      RolloutDevicePhase.FAILED,
      RolloutDevicePhase.SKIPPED,
      RolloutDevicePhase.EXCLUDED,
      99,
    ];
    const entries = phases.map((phase, index) =>
      create(MinerFirmwareHistoryEntrySchema, {
        ...completedHistoryEntry,
        rolloutId: BigInt(index + 1),
        firmwareVersion: `1.0.${index}`,
        rolloutStatus: RolloutStatus.CANCELED,
        phase,
        lastError: phase === RolloutDevicePhase.FAILED ? "Install failed" : "",
        skipNote: phase === RolloutDevicePhase.SKIPPED ? "Held for maintenance" : "",
      }),
    );
    render(<MinerFirmwareHistoryModalView {...propsFor(historyState({ entries }))} />);
    for (const [index, label] of ["Updated", "Failed", "Skipped", "Excluded", "Unknown"].entries()) {
      const row = screen.getByText(`1.0.${index}`).closest("tr")!;
      expect(within(row).getByText(label)).toBeInTheDocument();
    }
    expect(screen.getByText("Install failed")).toBeInTheDocument();
    expect(screen.getByText("Held for maintenance")).toBeInTheDocument();
    expect(screen.queryByText("Canceled")).not.toBeInTheDocument();
  });

  it("shows queued work as paused without replacing the miner's phase", () => {
    const entry = create(MinerFirmwareHistoryEntrySchema, {
      ...completedHistoryEntry,
      phase: RolloutDevicePhase.QUEUED,
      rolloutStatus: RolloutStatus.ACTIVE,
      paused: true,
    });
    render(<MinerFirmwareHistoryModalView {...propsFor(historyState({ entries: [entry] }))} />);
    expect(screen.getByText("Queued")).toBeInTheDocument();
    expect(screen.getByText("Update paused")).toBeInTheDocument();
  });

  it("offers page loading, refresh and retry while retaining transient-error rows", () => {
    const props = propsFor(historyState({ hasMore: true, error: "Connection timed out" }));
    render(<MinerFirmwareHistoryModalView {...props} />);
    expect(screen.getByRole("alert")).toHaveTextContent("Showing the last loaded history.");
    expect(screen.getByText("1.4.3")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Load older updates" }));
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(props.history.loadMore).toHaveBeenCalledOnce();
    expect(props.history.refresh).toHaveBeenCalledOnce();
    expect(props.history.retry).toHaveBeenCalledOnce();
  });

  it("distinguishes loading and empty history and disables paging during a request", () => {
    const props = propsFor(historyState({ entries: [], hasLoaded: false, isLoading: true, hasMore: true }));
    const { rerender } = render(<MinerFirmwareHistoryModalView {...props} />);
    expect(screen.getByRole("status")).toHaveTextContent("Loading firmware update history");
    expect(screen.getByRole("button", { name: "Load older updates" })).toBeDisabled();
    rerender(<MinerFirmwareHistoryModalView {...props} history={historyState({ entries: [] })} />);
    expect(screen.getByRole("status")).toHaveTextContent("No release channel updates for this miner yet.");
  });

  it("hides rows and navigation when permission is unavailable", () => {
    render(<MinerFirmwareHistoryModalView {...propsFor(historyState({ canRead: false }))} />);
    expect(screen.getByRole("status")).toHaveTextContent("unavailable with your current permissions");
    expect(screen.queryByText("1.4.3")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "View update" })).not.toBeInTheDocument();
  });

  it("opens the exact existing rollout detail URL", () => {
    vi.mocked(useMinerFirmwareHistory).mockReturnValue(historyState());
    function Location() {
      const location = useLocation();
      return <output data-testid="location">{location.pathname + location.search}</output>;
    }
    render(
      <MemoryRouter initialEntries={["/fleet"]}>
        <MinerFirmwareHistoryModal deviceIdentifier="miner-123" onClose={vi.fn()} />
        <Location />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "View update" }));
    expect(screen.getByTestId("location")).toHaveTextContent("/settings/firmware?tab=release-channels&rollout=42");
    expect(useMinerFirmwareHistory).toHaveBeenCalledWith("miner-123");
  });
});
