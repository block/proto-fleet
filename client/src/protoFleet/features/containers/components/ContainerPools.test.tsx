import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import ContainerPools from "./ContainerPools";
import type { ContainerPool } from "./PoolMonitorCard";

const makePool = (overrides: Partial<ContainerPool> = {}): ContainerPool => ({
  id: "pool-1",
  name: "Default pool",
  url: "mine.ocean.xyz:3334",
  role: "active",
  accepted: 90,
  rejected: 5,
  invalid: 5,
  difficulty: "524.3K",
  lastShare: "2s ago",
  bestShare: "165.6B",
  blocks: 3,
  ...overrides,
});

describe("ContainerPools", () => {
  it("defaults to 24h and reports time-range selections", async () => {
    const user = userEvent.setup();
    const onSelectDuration = vi.fn();
    render(<ContainerPools pools={[]} onSelectDuration={onSelectDuration} />);

    expect(screen.getByRole("button", { name: "24h" })).toHaveAttribute("aria-pressed", "true");

    await user.click(screen.getByRole("button", { name: "2d" }));

    expect(onSelectDuration).toHaveBeenCalledWith("2d");
    expect(screen.getByRole("button", { name: "24h" })).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByRole("button", { name: "2d" })).toHaveAttribute("aria-pressed", "true");
  });

  it("follows controlled duration changes", () => {
    const { rerender } = render(<ContainerPools pools={[]} duration="1h" />);

    expect(screen.getByRole("button", { name: "1h" })).toHaveAttribute("aria-pressed", "true");

    rerender(<ContainerPools pools={[]} duration="5d" />);

    expect(screen.getByRole("button", { name: "1h" })).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByRole("button", { name: "5d" })).toHaveAttribute("aria-pressed", "true");
  });

  it("gives prominence to the active pool instead of relying on array order", () => {
    render(
      <ContainerPools
        pools={[
          makePool({ id: "standby", name: "Backup 1", role: "standby" }),
          makePool({ id: "active", name: "Failover pool", role: "active" }),
        ]}
      />,
    );

    const cards = screen.getAllByTestId("pool-monitor-card");
    expect(cards[0]).toHaveAttribute("data-prominent", "false");
    expect(cards[1]).toHaveAttribute("data-prominent", "true");
    expect(screen.getByTestId("pool-monitor-panel")).toBe(cards[1].querySelector('[data-testid="pool-monitor-panel"]'));
  });

  it("renders normalized telemetry and accessible share composition", () => {
    render(
      <ContainerPools
        pools={[makePool({ accepted: Number.NaN, rejected: -2, invalid: Number.POSITIVE_INFINITY, blocks: -1 })]}
      />,
    );

    expect(screen.getByText("0%")).toBeInTheDocument();
    expect(screen.getAllByText("0")).toHaveLength(4);
    expect(screen.getByRole("progressbar", { name: "No data available" })).toBeInTheDocument();
  });
});
