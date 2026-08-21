import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import FleetHealthMetrics from "./FleetHealthMetrics";

describe("FleetHealthMetrics", () => {
  it("renders fleet size and per-status percentages with counts", () => {
    render(
      <FleetHealthMetrics
        fleetSize={200}
        healthyMiners={170}
        needsAttentionMiners={18}
        offlineMiners={8}
        sleepingMiners={4}
      />,
    );

    expect(screen.getByText("Your fleet")).toBeInTheDocument();
    expect(screen.getByText("200 miners")).toBeInTheDocument();

    expect(screen.getByText("85%")).toBeInTheDocument(); // 170/200
    expect(screen.getByText("9%")).toBeInTheDocument(); // 18/200
    expect(screen.getByText("4%")).toBeInTheDocument(); // 8/200
    expect(screen.getByText("2%")).toBeInTheDocument(); // 4/200

    expect(screen.getByText("170 miners")).toBeInTheDocument();
    expect(screen.getByText("18 miners")).toBeInTheDocument();
  });

  it("renders no card chrome or health bar — only the metric tiles", () => {
    const { container } = render(
      <FleetHealthMetrics
        fleetSize={50}
        healthyMiners={30}
        needsAttentionMiners={10}
        offlineMiners={7}
        sleepingMiners={3}
      />,
    );

    // The simplified list drops the CompositionBar that FleetHealth renders.
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
    expect(container.querySelector(".bg-surface-base")).not.toBeInTheDocument();
  });

  it("avoids division by zero when the fleet is empty", () => {
    render(
      <FleetHealthMetrics
        fleetSize={0}
        healthyMiners={0}
        needsAttentionMiners={0}
        offlineMiners={0}
        sleepingMiners={0}
      />,
    );

    expect(screen.getAllByText("0%")).toHaveLength(4);
  });

  it("shows skeletons while loading (any count undefined)", () => {
    render(<FleetHealthMetrics fleetSize={100} healthyMiners={70} needsAttentionMiners={10} />);

    expect(screen.getByText("Your fleet")).toBeInTheDocument();
    expect(screen.getAllByTestId("skeleton-bar")).toHaveLength(5);
    expect(screen.queryByText("70%")).not.toBeInTheDocument();
  });

  it("renders em-dashes for null counts (loaded but no data)", () => {
    render(
      <FleetHealthMetrics
        fleetSize={null}
        healthyMiners={null}
        needsAttentionMiners={null}
        offlineMiners={null}
        sleepingMiners={null}
      />,
    );

    expect(screen.getAllByText("—")).toHaveLength(5);
    expect(screen.queryByTestId("skeleton-bar")).not.toBeInTheDocument();
  });
});
