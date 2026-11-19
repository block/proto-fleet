import { MemoryRouter, useLocation } from "react-router-dom";
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import PoolStatusWrapper from "./PoolStatusWrapper";
import type { MinerAdapter } from "@/protoOS/api/adapters/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { asPoolId } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

const getMiningGroupsEnabled = vi.fn();
const listConfigurations = vi.fn();
const readConfigurations = vi.fn();
const nightShift = { id: "night-shift", name: "Night shift", revision: "1", assignedCount: 12 };
const adapter = {
  poolStatistics: { getMiningGroupsEnabled, list: listConfigurations, read: readConfigurations },
} as unknown as MinerAdapter;

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

function CurrentPath() {
  return <div data-testid="current-path">{useLocation().pathname}</div>;
}

function renderWidget(hosting: Partial<ReturnType<typeof useMinerHosting>> = {}) {
  vi.mocked(useMinerHosting).mockReturnValue({ minerRoot: "", ...hosting } as ReturnType<typeof useMinerHosting>);
  return render(
    <MemoryRouter>
      <PoolStatusWrapper />
      <CurrentPath />
    </MemoryRouter>,
  );
}

async function renderCt1Widget() {
  renderWidget({ adapter });
  await act(async () => {});
  return screen.getByRole("button", { name: "Mining pools" });
}

describe("PoolStatusWrapper view-pools navigation", () => {
  beforeEach(() => {
    getMiningGroupsEnabled.mockReset().mockResolvedValue(true);
    listConfigurations.mockReset().mockResolvedValue({ enabled: true, configurations: [] });
    readConfigurations.mockReset().mockResolvedValue([]);
    useMinerStore
      .getState()
      .pools.setPoolsInfo([{ id: asPoolId("pool:1"), priority: 0, url: "default.example.com", status: "Dead" }]);
  });

  test.each([
    { mode: "Direct R2", hosting: {}, settingsPath: "/settings/mining-pools" },
    {
      mode: "Fleet-hosted mode",
      hosting: { minerRoot: "/miners/miner-1", isFleetHosted: true },
      settingsPath: "/miners/miner-1/settings/mining-pools",
    },
  ])("$mode opens its pool settings", ({ hosting, settingsPath }) => {
    renderWidget(hosting);
    fireEvent.click(screen.getByRole("button", { name: "Mining Pool" }));
    fireEvent.click(screen.getByText("View mining pools"));
    expect(screen.getByTestId("current-path").textContent).toBe(settingsPath);
  });

  test("CT1 with mining groups disabled keeps the single-pool status and settings link", async () => {
    getMiningGroupsEnabled.mockResolvedValue(false);
    renderWidget({ adapter });
    fireEvent.click(await screen.findByRole("button", { name: "Mining Pool" }));
    expect(await screen.findByText("Not connected")).toBeInTheDocument();
    expect(getMiningGroupsEnabled).toHaveBeenCalledTimes(2);
    expect(listConfigurations).not.toHaveBeenCalled();
    fireEvent.click(screen.getByText("View mining pools"));
    expect(screen.getByTestId("current-path")).toHaveTextContent("/settings/mining-pools");
  });

  test("shows the popup while rechecking a disabled mode", async () => {
    let finishModeCheck!: (enabled: boolean) => void;
    const modeCheck = new Promise<boolean>((resolve) => {
      finishModeCheck = resolve;
    });
    getMiningGroupsEnabled.mockResolvedValueOnce(false).mockReturnValueOnce(modeCheck);
    renderWidget({ adapter });

    fireEvent.click(await screen.findByRole("button", { name: "Mining Pool" }));
    const popover = within(screen.getByTestId("pool-info-popover"));
    expect(popover.getByText("Checking mining group status…")).toBeInTheDocument();
    expect(popover.getByText("View pools").closest("button")).toBeEnabled();
    expect(popover.queryByText("Not connected")).not.toBeInTheDocument();
    expect(popover.queryByText("View mining pools")).not.toBeInTheDocument();

    await act(async () => finishModeCheck(true));
    expect(await screen.findByText("No hashboards assigned.")).toBeInTheDocument();
    expect(getMiningGroupsEnabled).toHaveBeenCalledTimes(2);
    fireEvent.click(screen.getByText("View pools"));
    expect(screen.getByTestId("current-path").textContent).toBe("/pools");
  });

  test("uses legacy status when the assignment read confirms mining groups are disabled", async () => {
    vi.useFakeTimers();
    try {
      let finishOlderPoll!: (enabled: boolean) => void;
      const olderPoll = new Promise<boolean>((resolve) => {
        finishOlderPoll = resolve;
      });
      getMiningGroupsEnabled.mockResolvedValueOnce(true).mockReturnValueOnce(olderPoll);
      listConfigurations.mockResolvedValueOnce({ enabled: false, configurations: [] });

      const trigger = await renderCt1Widget();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(15_000);
      });
      expect(getMiningGroupsEnabled).toHaveBeenCalledTimes(2);

      await act(async () => fireEvent.click(trigger));
      expect(screen.getByText("Not connected")).toBeInTheDocument();

      await act(async () => finishOlderPoll(true));
      expect(listConfigurations).toHaveBeenCalledTimes(1);
      fireEvent.click(screen.getByText("View mining pools"));
      expect(screen.getByTestId("current-path").textContent).toBe("/settings/mining-pools");
    } finally {
      vi.useRealTimers();
    }
  });

  test.each(["enabled", "unavailable"])("CT1 opens Pools when mining-group mode is %s", async (mode) => {
    if (mode === "unavailable") getMiningGroupsEnabled.mockRejectedValue(new Error("offline"));
    fireEvent.click(await renderCt1Widget());
    const popover = within(screen.getByTestId("pool-info-popover"));
    expect(popover.queryByText("default.example.com")).not.toBeInTheDocument();
    expect(popover.queryByText("Not connected")).not.toBeInTheDocument();
    fireEvent.click(popover.getByText("View pools"));
    expect(screen.getByTestId("current-path").textContent).toBe("/pools");
  });

  test("loads current assignments on each open", async () => {
    listConfigurations
      .mockResolvedValueOnce({
        enabled: true,
        configurations: [
          { name: "Default", assignedCount: 7 },
          { name: "Night shift", assignedCount: 5 },
        ],
      })
      .mockResolvedValueOnce({ enabled: true, configurations: [nightShift] });
    const trigger = await renderCt1Widget();
    expect(listConfigurations).not.toHaveBeenCalled();

    fireEvent.click(trigger);
    expect(await screen.findByText("12 hashboards across 2 pool configurations")).toBeInTheDocument();
    expect(readConfigurations).not.toHaveBeenCalled();

    fireEvent.click(trigger);
    fireEvent.click(trigger);
    expect(await screen.findByText("Night shift · 12 hashboards")).toBeInTheDocument();
    expect(listConfigurations).toHaveBeenCalledTimes(2);
    expect(readConfigurations).toHaveBeenCalledExactlyOnceWith([nightShift], expect.anything());
  });

  test("shows every active pool and reporting coverage for the single assigned configuration", async () => {
    listConfigurations.mockResolvedValue({ enabled: true, configurations: [nightShift] });
    readConfigurations.mockResolvedValue([
      {
        ...nightShift,
        observedBoards: 8,
        pools: [
          { id: asPoolId("pool:2"), name: "Primary", url: "primary.example.com", status: "Active", priority: 0 },
          { id: asPoolId("pool:3"), name: "Secondary", url: "secondary.example.com", status: "Alive", priority: 1 },
          { id: asPoolId("pool:4"), url: "backup.example.com", status: "Idle", priority: 2 },
        ],
      },
    ]);

    fireEvent.click(await renderCt1Widget());
    expect(await screen.findByText("Connected")).toBeInTheDocument();
    expect(screen.getByText("Primary")).toBeInTheDocument();
    expect(screen.getByText("Secondary")).toBeInTheDocument();
    expect(screen.getByText("primary.example.com")).toBeInTheDocument();
    expect(screen.getByText("secondary.example.com")).toBeInTheDocument();
    expect(screen.queryByText("backup.example.com")).not.toBeInTheDocument();
    expect(screen.getByText("This controller observes 8 of 12 assigned hashboards.")).toBeInTheDocument();
  });

  test("keeps the assignment summary and View pools when the pool readings are unavailable", async () => {
    listConfigurations.mockResolvedValue({ enabled: true, configurations: [nightShift] });
    readConfigurations.mockRejectedValueOnce(new Error("offline"));
    const trigger = await renderCt1Widget();

    await act(async () => fireEvent.click(trigger));
    expect(readConfigurations).toHaveBeenCalledExactlyOnceWith(
      [nightShift],
      expect.objectContaining({ includeLastRecordedShares: false }),
    );
    expect(screen.getByText("Night shift · 12 hashboards")).toBeInTheDocument();
    expect(screen.queryByText("Connected")).not.toBeInTheDocument();
    fireEvent.click(screen.getByText("View pools"));
    expect(screen.getByTestId("current-path").textContent).toBe("/pools");
  });

  test("replaces the old assignment summary when the configuration changes during the read", async () => {
    listConfigurations.mockResolvedValue({ enabled: true, configurations: [nightShift] });
    readConfigurations.mockResolvedValue([{ ...nightShift, pools: [], stale: true }]);

    fireEvent.click(await renderCt1Widget());
    expect(await screen.findByText("Configuration changed")).toBeInTheDocument();
    expect(screen.queryByText("Night shift · 12 hashboards")).not.toBeInTheDocument();
    fireEvent.click(screen.getByText("View pools"));
    expect(screen.getByTestId("current-path").textContent).toBe("/pools");
  });

  test("shows reporting coverage when no hashboards report pool status", async () => {
    listConfigurations.mockResolvedValue({ enabled: true, configurations: [nightShift] });
    readConfigurations.mockResolvedValue([
      {
        ...nightShift,
        observedBoards: 0,
        pools: [{ id: asPoolId("pool:2"), url: "primary.example.com", status: "Unknown", priority: 0 }],
      },
    ]);

    fireEvent.click(await renderCt1Widget());
    expect(await screen.findByText("This controller observes 0 of 12 assigned hashboards.")).toBeInTheDocument();
    expect(screen.queryByText("Connected")).not.toBeInTheDocument();
  });

  test("keeps View pools available when the assignment summary is unavailable", async () => {
    listConfigurations.mockRejectedValueOnce(new Error("offline"));

    fireEvent.click(await renderCt1Widget());
    expect(screen.getByText("Loading assignments…")).toBeInTheDocument();
    expect(screen.getByText("View pools")).toBeInTheDocument();
    expect(await screen.findByText("Assignment summary unavailable.")).toBeInTheDocument();
    fireEvent.click(screen.getByText("View pools"));
    expect(screen.getByTestId("current-path").textContent).toBe("/pools");
  });
});
