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
const adapter = { poolStatistics: { getMiningGroupsEnabled, list: listConfigurations } } as unknown as MinerAdapter;

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

describe("PoolStatusWrapper view-pools navigation", () => {
  beforeEach(() => {
    getMiningGroupsEnabled.mockReset().mockResolvedValue(true);
    listConfigurations.mockReset().mockResolvedValue({ enabled: true, configurations: [] });
    useMinerStore
      .getState()
      .pools.setPoolsInfo([{ id: asPoolId("pool:1"), priority: 0, url: "default.example.com", status: "Dead" }]);
  });

  test("direct R2 opens pool settings", () => {
    renderWidget();
    fireEvent.click(screen.getByRole("button", { name: "Mining Pool" }));
    fireEvent.click(screen.getByText("View mining pools"));
    expect(screen.getByTestId("current-path")).toHaveTextContent("/settings/mining-pools");
  });

  test("Fleet-hosted mode opens pool settings inside the miner view", () => {
    renderWidget({ minerRoot: "/miners/miner-1", isFleetHosted: true });
    fireEvent.click(screen.getByRole("button", { name: "Mining Pool" }));
    fireEvent.click(screen.getByText("View mining pools"));
    expect(screen.getByTestId("current-path")).toHaveTextContent("/miners/miner-1/settings/mining-pools");
  });

  test("CT1 with mining groups disabled keeps the single-pool status and settings link", async () => {
    getMiningGroupsEnabled.mockResolvedValue(false);
    renderWidget({ adapter });
    fireEvent.click(await screen.findByRole("button", { name: "Mining Pool" }));
    expect(screen.getByText("Not connected")).toBeInTheDocument();
    expect(listConfigurations).not.toHaveBeenCalled();
    fireEvent.click(screen.getByText("View mining pools"));
    expect(screen.getByTestId("current-path")).toHaveTextContent("/settings/mining-pools");
  });

  test.each(["enabled", "unavailable"])("CT1 opens Pools when mining-group mode is %s", async (mode) => {
    if (mode === "unavailable") getMiningGroupsEnabled.mockRejectedValue(new Error("offline"));
    renderWidget({ adapter });
    await act(async () => {});
    fireEvent.click(screen.getByRole("button", { name: "Mining pools" }));
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
      .mockResolvedValueOnce({ enabled: true, configurations: [{ name: "Night shift", assignedCount: 12 }] });
    renderWidget({ adapter });
    await act(async () => {});
    expect(listConfigurations).not.toHaveBeenCalled();

    const trigger = screen.getByRole("button", { name: "Mining pools" });
    fireEvent.click(trigger);
    expect(await screen.findByText("12 hashboards across 2 pool configurations")).toBeInTheDocument();

    fireEvent.click(trigger);
    fireEvent.click(trigger);
    expect(await screen.findByText("Night shift · 12 hashboards")).toBeInTheDocument();
    expect(listConfigurations).toHaveBeenCalledTimes(2);
  });

  test("keeps View pools available when the assignment summary is unavailable", async () => {
    listConfigurations.mockRejectedValueOnce(new Error("offline"));
    renderWidget({ adapter });
    await act(async () => {});

    fireEvent.click(screen.getByRole("button", { name: "Mining pools" }));
    expect(screen.getByText("Loading assignments…")).toBeInTheDocument();
    expect(screen.getByText("View pools")).toBeInTheDocument();
    expect(await screen.findByText("Assignment summary unavailable.")).toBeInTheDocument();
    fireEvent.click(screen.getByText("View pools"));
    expect(screen.getByTestId("current-path").textContent).toBe("/pools");
  });
});
