import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import PoolStatisticsView from "./PoolStatisticsView";
import { asPoolId } from "@/protoOS/store/contract";

const pool = {
  id: asPoolId("pool:1"),
  name: "Primary",
  priority: 0,
  status: "Active",
  difficulty: 0,
  url: "stratum+tcp://pool.example:3333",
  username: "ct1.primary-worker",
};
describe("Pools statistics view", () => {
  it("shows real zeros, unavailable metrics, correct status, and embedded configuration navigation", () => {
    render(
      <MemoryRouter>
        <PoolStatisticsView
          pools={[pool, { ...pool, id: asPoolId("pool:2"), name: "Backup", status: "Idle", username: undefined }]}
          history={{ byPool: { [pool.id]: { accepted: 0, rejected: 0, partial: false } }, unavailable: [] }}
          duration="24h"
          onDurationChange={vi.fn()}
          configureHref="/miners/ct1/settings/mining-pools"
        />
      </MemoryRouter>,
    );
    const primary = screen.getByRole("region", { name: "Primary" });
    expect(within(primary).getByText("Active")).toBeInTheDocument();
    expect(within(primary).getAllByText("0")).toHaveLength(3);
    expect(within(primary).getAllByText("—")).toHaveLength(5);
    expect(screen.getByText("Standby")).toBeInTheDocument();
    expect(within(primary).getByText("Worker: ct1.primary-worker")).toBeInTheDocument();
    expect(within(screen.getByRole("region", { name: "Backup" })).getByText("Worker: —")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Configure pools" })).toHaveAttribute(
      "href",
      "/miners/ct1/settings/mining-pools",
    );
    expect(screen.getByRole("img")).toHaveAccessibleName("Share proportions unavailable");
  });

  it("changes ranges and exposes partial data and errors", () => {
    const change = vi.fn();
    render(
      <MemoryRouter>
        <PoolStatisticsView
          pools={[pool]}
          history={{
            byPool: { [pool.id]: { accepted: 9, rejected: 1, acceptanceRate: 0.9, partial: true } },
            unavailable: [],
          }}
          duration="24h"
          onDurationChange={change}
          configureHref="/settings/mining-pools"
          error="Could not refresh"
        />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "48h" }));
    expect(change).toHaveBeenCalledWith("48h");
    expect(screen.getByRole("button", { name: "24h" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("img")).toHaveAccessibleName("90.0% accepted, 10.0% rejected");
    expect(screen.getByText("Partial share history for this period.")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("Could not refresh");
  });

  it("offers configuration for an empty pool list", () => {
    render(
      <MemoryRouter>
        <PoolStatisticsView
          pools={[]}
          duration="24h"
          onDurationChange={vi.fn()}
          configureHref="/settings/mining-pools"
        />
      </MemoryRouter>,
    );
    expect(screen.getByText(/No pools configured/)).toBeInTheDocument();
  });
});
