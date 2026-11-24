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

  it("shows the recorded result time, outcome, and assigned target difficulty", () => {
    const resultAtMs = Date.parse("2026-09-29T02:00:00Z");
    render(
      <MemoryRouter>
        <PoolStatisticsView
          pools={[{ ...pool, lastShareAtMs: resultAtMs, lastAssignedDifficulty: "2048", lastShareOutcome: "rejected" }]}
          duration="24h"
          onDurationChange={vi.fn()}
          configureHref="/settings/mining-pools"
        />
      </MemoryRouter>,
    );
    const section = screen.getByRole("region", { name: "Primary" });
    expect(within(section).getByRole("heading", { name: "Last result time" })).toBeInTheDocument();
    expect(
      within(section).getByText(
        new Date(resultAtMs).toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" }),
      ),
    ).toBeInTheDocument();
    expect(within(section).getAllByText("Rejected")).toHaveLength(3);
    expect(within(section).getByRole("heading", { name: "Last assigned difficulty" })).toBeInTheDocument();
    expect(within(section).getByText("2K")).toBeInTheDocument();
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

  it("labels missing observations without presenting them as zero shares", () => {
    render(
      <MemoryRouter>
        <PoolStatisticsView
          pools={[{ ...pool, difficulty: undefined }]}
          history={{ byPool: { [pool.id]: { partial: true } }, unavailable: [] }}
          duration="24h"
          onDurationChange={vi.fn()}
          configureHref="/settings/mining-pools"
        />
      </MemoryRouter>,
    );
    expect(screen.getByText("Partial share history for this period.")).toBeInTheDocument();
    expect(screen.queryByText("0")).not.toBeInTheDocument();
    expect(screen.getByRole("img")).toHaveAccessibleName("Share proportions unavailable");
  });
});
