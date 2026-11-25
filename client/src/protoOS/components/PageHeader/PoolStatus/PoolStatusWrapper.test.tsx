import type { ReactNode } from "react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import PoolStatusWrapper from "./PoolStatusWrapper";
import type { MinerAdapter } from "@/protoOS/api/adapters/types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { usePoolConfigurationStatistics } from "@/protoOS/features/pools/usePoolConfigurationStatistics";
import { asPoolId } from "@/protoOS/store/contract";
import { beginPoolConfigurationModeRead } from "@/protoOS/store/hooks/usePoolConfigurationMode";
import useMinerStore from "@/protoOS/store/useMinerStore";

const getMiningGroupsEnabled = vi.fn();
const listConfigurations = vi.fn();
const readConfigurations = vi.fn();
const nightShift = { id: "night-shift", name: "Night shift", revision: "1", assignedCount: 12 };
const statistics = { getMiningGroupsEnabled, list: listConfigurations, read: readConfigurations };
const adapter = { poolStatistics: statistics } as unknown as MinerAdapter;

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

function CurrentPath() {
  return <div data-testid="current-path">{useLocation().pathname}</div>;
}

function renderWidget(
  hosting: Partial<ReturnType<typeof useMinerHosting>> = {},
  { path = "/", children }: { path?: string; children?: ReactNode } = {},
) {
  vi.mocked(useMinerHosting).mockReturnValue({ minerRoot: "", ...hosting } as ReturnType<typeof useMinerHosting>);
  return render(
    <MemoryRouter initialEntries={[path]}>
      <PoolStatusWrapper />
      {children}
      <CurrentPath />
    </MemoryRouter>,
  );
}

function PoolsPageCatalogue() {
  usePoolConfigurationStatistics(statistics, "24h", 0);
  return null;
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
    useMinerStore.getState().resetDeviceData();
    useMinerStore
      .getState()
      .pools.setPoolsInfo([{ id: asPoolId("pool:1"), priority: 0, url: "default.example.com", status: "Dead" }]);
  });

  test.each([
    { mode: "Direct R2", hosting: {}, settingsPath: "/settings/mining-pools" },
    {
      mode: "Fleet-hosted mode",
      hosting: { adapter, minerRoot: "/miners/miner-1", isFleetHosted: true },
      settingsPath: "/miners/miner-1/settings/mining-pools",
    },
  ])("$mode opens its pool settings", ({ hosting, settingsPath }) => {
    renderWidget(hosting);
    fireEvent.click(screen.getByRole("button", { name: "Mining Pool" }));
    fireEvent.click(screen.getByText("View mining pools"));
    expect(screen.getByTestId("current-path").textContent).toBe(settingsPath);
    expect(getMiningGroupsEnabled).not.toHaveBeenCalled();
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

  test("keeps the legacy popup visible while rechecking a disabled mode", async () => {
    let finishModeCheck!: (enabled: boolean) => void;
    const modeCheck = new Promise<boolean>((resolve) => {
      finishModeCheck = resolve;
    });
    getMiningGroupsEnabled.mockResolvedValueOnce(false).mockReturnValueOnce(modeCheck);
    renderWidget({ adapter });

    fireEvent.click(await screen.findByRole("button", { name: "Mining Pool" }));
    const popover = within(screen.getByTestId("pool-info-popover"));
    expect(popover.getByText("Not connected")).toBeInTheDocument();
    expect(popover.getByText("View mining pools")).toBeInTheDocument();

    await act(async () => finishModeCheck(true));
    expect(await screen.findByText("No hashboards assigned.")).toBeInTheDocument();
    expect(getMiningGroupsEnabled).toHaveBeenCalledTimes(2);
    fireEvent.click(screen.getByText("View pools"));
    expect(screen.getByTestId("current-path").textContent).toBe("/pools");
  });

  test("keeps legacy status and navigation after a mode recheck fails", async () => {
    getMiningGroupsEnabled.mockResolvedValueOnce(false).mockRejectedValueOnce(new Error("offline"));
    renderWidget({ adapter });

    const trigger = await screen.findByRole("button", { name: "Mining Pool" });
    await act(async () => fireEvent.click(trigger));
    expect(getMiningGroupsEnabled).toHaveBeenCalledTimes(2);
    expect(screen.getByText("Not connected")).toBeInTheDocument();
    fireEvent.click(screen.getByText("View mining pools"));
    expect(screen.getByTestId("current-path").textContent).toBe("/settings/mining-pools");
  });

  test("waits for the first mode check before opening the popup", async () => {
    let finishModeCheck!: (enabled: boolean) => void;
    const modeCheck = new Promise<boolean>((resolve) => {
      finishModeCheck = resolve;
    });
    getMiningGroupsEnabled.mockReturnValueOnce(modeCheck).mockResolvedValue(false);
    renderWidget({ adapter });

    const trigger = screen.getByRole("button", { name: "Mining Pool" });
    fireEvent.click(trigger);
    expect(screen.queryByTestId("pool-info-popover")).not.toBeInTheDocument();

    await act(async () => finishModeCheck(false));
    fireEvent.click(trigger);
    expect(await screen.findByText("View mining pools")).toBeInTheDocument();
  });

  test("retries the initial mode check without blocking the fallback action", async () => {
    let finishRetry!: (enabled: boolean) => void;
    const retry = new Promise<boolean>((resolve) => {
      finishRetry = resolve;
    });
    getMiningGroupsEnabled.mockRejectedValueOnce(new Error("offline")).mockReturnValueOnce(retry);
    renderWidget({ adapter });

    const trigger = await screen.findByRole("button", { name: "Mining pools" });
    expect(trigger).not.toHaveClass("hover:cursor-progress");
    expect(getMiningGroupsEnabled).toHaveBeenCalledTimes(2);
    await act(async () => finishRetry(false));

    expect(screen.getByRole("button", { name: "Mining Pool" })).toBeInTheDocument();
    expect(getMiningGroupsEnabled).toHaveBeenCalledTimes(2);
  });

  test("uses the Pools page assignment read for the initial mode", async () => {
    renderWidget({ adapter }, { path: "/pools", children: <PoolsPageCatalogue /> });

    expect(await screen.findByRole("button", { name: "Mining pools" })).toBeInTheDocument();
    expect(listConfigurations).toHaveBeenCalledTimes(1);
    expect(getMiningGroupsEnabled).not.toHaveBeenCalled();
  });

  test("uses legacy status despite an older enabled response", async () => {
    listConfigurations.mockResolvedValueOnce({ enabled: false, configurations: [] });
    const trigger = await renderCt1Widget();
    const recordOlderMode = beginPoolConfigurationModeRead(statistics, new AbortController().signal);

    await act(async () => fireEvent.click(trigger));
    expect(screen.getByText("Not connected")).toBeInTheDocument();

    await act(async () => recordOlderMode(true));
    expect(listConfigurations).toHaveBeenCalledTimes(1);
    expect(getMiningGroupsEnabled).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByText("View mining pools"));
    expect(screen.getByTestId("current-path").textContent).toBe("/settings/mining-pools");
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

  test("labels unnamed active pools by their original position and excludes inactive pools", async () => {
    listConfigurations.mockResolvedValue({ enabled: true, configurations: [nightShift] });
    readConfigurations.mockResolvedValue([
      {
        ...nightShift,
        pools: [
          { id: asPoolId("pool:1"), url: "primary.example.com", status: "aLiVe", priority: 1 },
          { id: asPoolId("pool:2"), url: "standby.example.com", status: "Inactive", priority: 5 },
          { id: asPoolId("pool:3"), url: "backup.example.com", status: "aCtIvE", priority: 8 },
        ],
      },
    ]);

    fireEvent.click(await renderCt1Widget());
    expect(await screen.findByText("Default Pool")).toBeInTheDocument();
    expect(screen.getByText("Backup Pool #2")).toBeInTheDocument();
    expect(screen.queryByText("standby.example.com")).not.toBeInTheDocument();
  });

  test("shows pending status until the pool read confirms no active pool", async () => {
    listConfigurations.mockResolvedValue({ enabled: true, configurations: [nightShift] });
    const readings = [
      {
        ...nightShift,
        observedBoards: 12,
        pools: [{ id: asPoolId("pool:1"), url: "primary.example.com", status: "Dead", priority: 0 }],
      },
    ];
    let finishRead!: (value: typeof readings) => void;
    readConfigurations.mockReturnValueOnce(
      new Promise<typeof readings>((resolve) => {
        finishRead = resolve;
      }),
    );

    fireEvent.click(await renderCt1Widget());
    expect(await screen.findByText("Loading pool status…")).toBeInTheDocument();
    expect(screen.getByText("Night shift · 12 hashboards")).toBeInTheDocument();
    await act(async () => finishRead(readings));
    expect(screen.getByText("No active pool")).toBeInTheDocument();
    expect(screen.queryByText("Loading pool status…")).not.toBeInTheDocument();
  });

  test.each(["rejects", "returns an error"])("keeps the summary and View pools when the read %s", async (failure) => {
    listConfigurations.mockResolvedValue({ enabled: true, configurations: [nightShift] });
    if (failure === "rejects") {
      readConfigurations.mockRejectedValueOnce(new Error("offline"));
    } else {
      readConfigurations.mockResolvedValueOnce([
        { ...nightShift, pools: [], error: "Connection status is unavailable." },
      ]);
    }
    const trigger = await renderCt1Widget();

    await act(async () => fireEvent.click(trigger));
    expect(readConfigurations).toHaveBeenCalledExactlyOnceWith(
      [nightShift],
      expect.objectContaining({ includeLastRecordedShares: false }),
    );
    expect(screen.getByText("Night shift · 12 hashboards")).toBeInTheDocument();
    expect(screen.getByText("Pool status unavailable")).toBeInTheDocument();
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
    expect(screen.getByText("Pool status unavailable")).toBeInTheDocument();
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
