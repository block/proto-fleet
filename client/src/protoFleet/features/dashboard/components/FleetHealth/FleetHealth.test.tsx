import React from "react";
import { BrowserRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import FleetHealth from "./FleetHealth";

describe("FleetHealth", () => {
  const renderWithRouter = (component: React.ReactElement) => {
    return render(<BrowserRouter>{component}</BrowserRouter>);
  };

  it("renders correct stats when all miners are healthy", () => {
    renderWithRouter(
      <FleetHealth fleetSize={100} healthyMiners={100} needsAttentionMiners={0} offlineMiners={0} sleepingMiners={0} />,
    );

    // Check title label
    expect(screen.getByText("Your fleet")).toBeInTheDocument();

    // Check percentages
    expect(screen.getByText("100%")).toBeInTheDocument(); // Healthy

    // Check counts - "100 miners" appears twice (header and healthy column)
    const healthyCount = screen.getAllByText("100 miners");
    expect(healthyCount).toHaveLength(2); // One in header, one in healthy column

    const zeroMiners = screen.getAllByText("0 miners");
    expect(zeroMiners).toHaveLength(3); // Needs Attention, Offline, and Sleeping columns

    // Check that legend is present
    const healthyTexts = screen.getAllByText("Healthy");
    expect(healthyTexts.length).toBeGreaterThan(0);
    const needsAttentionTexts = screen.getAllByText("Needs Attention");
    expect(needsAttentionTexts.length).toBeGreaterThan(0);
    const offlineTexts = screen.getAllByText("Offline");
    expect(offlineTexts.length).toBeGreaterThan(0);
    const sleepingTexts = screen.getAllByText("Sleeping");
    expect(sleepingTexts.length).toBeGreaterThan(0);

    // Check CompositionBar is rendered
    const progressBars = screen.getAllByRole("progressbar");
    expect(progressBars.length).toBeGreaterThan(0);
  });

  it("renders correct stats with mixed fleet health", () => {
    renderWithRouter(
      <FleetHealth
        fleetSize={200}
        healthyMiners={170}
        needsAttentionMiners={18}
        offlineMiners={8}
        sleepingMiners={4}
      />,
    );

    // Check miner count
    expect(screen.getByText("200 miners")).toBeInTheDocument();

    // Check percentages (85% + 9% + 4% + 2% = 100%)
    expect(screen.getByText("85%")).toBeInTheDocument(); // Healthy: 170/200 = 85%
    expect(screen.getByText("9%")).toBeInTheDocument(); // Needs Attention: 18/200 = 9%
    expect(screen.getByText("4%")).toBeInTheDocument(); // Offline: 8/200 = 4%
    expect(screen.getByText("2%")).toBeInTheDocument(); // Sleeping: 4/200 = 2%

    // Check miner counts
    expect(screen.getByText("170 miners")).toBeInTheDocument();
    expect(screen.getByText("18 miners")).toBeInTheDocument();
    expect(screen.getByText("8 miners")).toBeInTheDocument();
    expect(screen.getByText("4 miners")).toBeInTheDocument();
  });

  it("renders stats for fleet with moderate health distribution", () => {
    renderWithRouter(
      <FleetHealth
        fleetSize={100}
        healthyMiners={60}
        needsAttentionMiners={20}
        offlineMiners={12}
        sleepingMiners={8}
      />,
    );

    // Check title label
    expect(screen.getByText("Your fleet")).toBeInTheDocument();

    // Check percentages (all unique values)
    expect(screen.getByText("60%")).toBeInTheDocument(); // Healthy: 60/100 = 60%
    expect(screen.getByText("20%")).toBeInTheDocument(); // Needs Attention: 20/100 = 20%
    expect(screen.getByText("12%")).toBeInTheDocument(); // Offline: 12/100 = 12%
    expect(screen.getByText("8%")).toBeInTheDocument(); // Sleeping: 8/100 = 8%
  });

  it("renders stats for fleet with critical health distribution", () => {
    renderWithRouter(
      <FleetHealth
        fleetSize={100}
        healthyMiners={15}
        needsAttentionMiners={50}
        offlineMiners={25}
        sleepingMiners={10}
      />,
    );

    // Check title label
    expect(screen.getByText("Your fleet")).toBeInTheDocument();

    // Check percentages (all unique values)
    expect(screen.getByText("15%")).toBeInTheDocument(); // Healthy: 15/100 = 15%
    expect(screen.getByText("50%")).toBeInTheDocument(); // Needs Attention: 50/100 = 50%
    expect(screen.getByText("25%")).toBeInTheDocument(); // Offline: 25/100 = 25%
    expect(screen.getByText("10%")).toBeInTheDocument(); // Sleeping: 10/100 = 10%
  });

  it("handles division by zero when fleet size is 0", () => {
    renderWithRouter(
      <FleetHealth fleetSize={0} healthyMiners={0} needsAttentionMiners={0} offlineMiners={0} sleepingMiners={0} />,
    );

    // Should render without errors
    expect(screen.getByText("Your fleet")).toBeInTheDocument();

    // All percentages should be 0%
    const zeroPercents = screen.getAllByText("0%");
    expect(zeroPercents).toHaveLength(4); // Healthy, Needs Attention, Offline, Sleeping

    // All miner counts should be 0 miners (4 in columns, 1 in header = 5 total)
    const zeroMinerCounts = screen.getAllByText("0 miners");
    expect(zeroMinerCounts).toHaveLength(5);
  });

  it("renders loading state when miner counts are undefined", () => {
    renderWithRouter(<FleetHealth />);

    // Should render skeleton bars instead of values
    expect(screen.getByText("Your fleet")).toBeInTheDocument();

    // Check that all stat labels are present but with skeleton bars
    const healthyTexts = screen.getAllByText("Healthy");
    expect(healthyTexts.length).toBeGreaterThan(0);
    const needsAttentionTexts = screen.getAllByText("Needs Attention");
    expect(needsAttentionTexts.length).toBeGreaterThan(0);
    const offlineTexts = screen.getAllByText("Offline");
    expect(offlineTexts.length).toBeGreaterThan(0);
    const sleepingTexts = screen.getAllByText("Sleeping");
    expect(sleepingTexts.length).toBeGreaterThan(0);

    // Skeleton bars should be present:
    // - 5 for stat values (Your fleet + 4 health categories)
    // - 5 for composition bar area (1 bar + 4 legend items)
    const skeletonBars = screen.getAllByTestId("skeleton-bar");
    expect(skeletonBars.length).toBe(10);
  });

  it("renders full loading state when some props are undefined", () => {
    renderWithRouter(
      <FleetHealth
        fleetSize={100}
        healthyMiners={70}
        needsAttentionMiners={10}
        // offlineMiners and sleepingMiners are undefined
      />,
    );

    // Check title label is present
    expect(screen.getByText("Your fleet")).toBeInTheDocument();

    // When ANY prop is undefined, show full loading skeleton
    // This provides consistent UX rather than showing partial/incomplete data
    const skeletonBars = screen.getAllByTestId("skeleton-bar");
    expect(skeletonBars.length).toBe(10); // Full loading state (5 stats + 5 composition bar area)

    // Defined values should NOT be shown (we're in loading state)
    expect(screen.queryByText("70%")).not.toBeInTheDocument();
    expect(screen.queryByText("70 miners")).not.toBeInTheDocument();
  });

  it("renders legend with correct color indicators", () => {
    const { container } = renderWithRouter(
      <FleetHealth
        fleetSize={100}
        healthyMiners={70}
        needsAttentionMiners={15}
        offlineMiners={10}
        sleepingMiners={5}
      />,
    );

    // Check legend items
    const healthyTexts = screen.getAllByText("Healthy");
    expect(healthyTexts.length).toBeGreaterThan(0);
    const needsAttentionTexts = screen.getAllByText("Needs Attention");
    expect(needsAttentionTexts.length).toBeGreaterThan(0);
    const offlineTexts = screen.getAllByText("Offline");
    expect(offlineTexts.length).toBeGreaterThan(0);
    const sleepingTexts = screen.getAllByText("Sleeping");
    expect(sleepingTexts.length).toBeGreaterThan(0);

    // Check that the triangle SVG exists for needs attention
    const svgTriangle = container.querySelector("svg");
    expect(svgTriangle).toBeInTheDocument();

    // Check color indicators
    const greenIndicators = container.querySelectorAll(".bg-core-primary-fill");
    const redIndicators = container.querySelectorAll(".fill-intent-critical-fill, .text-intent-critical-fill");
    const accentIndicators = container.querySelectorAll(".bg-core-accent-fill");
    const primaryIndicators = container.querySelectorAll(".bg-core-primary-20");

    expect(greenIndicators.length).toBeGreaterThan(0); // Healthy
    expect(redIndicators.length).toBeGreaterThan(0); // Needs Attention
    expect(accentIndicators.length).toBeGreaterThan(0); // Offline
    expect(primaryIndicators.length).toBeGreaterThan(0); // Sleeping
  });

  it("handles pluralization correctly for singular miner", () => {
    renderWithRouter(
      <FleetHealth fleetSize={1} healthyMiners={0} needsAttentionMiners={1} offlineMiners={0} sleepingMiners={0} />,
    );

    // Check singular form - should appear in title and Needs Attention stat
    const oneMinerText = screen.getAllByText(/1 miner/);
    expect(oneMinerText.length).toBe(2); // Once in title, once in Needs Attention stat
  });

  it("handles pluralization correctly for multiple miners", () => {
    renderWithRouter(
      <FleetHealth fleetSize={50} healthyMiners={30} needsAttentionMiners={10} offlineMiners={7} sleepingMiners={3} />,
    );

    // Check plural form
    expect(screen.getByText("50 miners")).toBeInTheDocument();
    expect(screen.getByText("30 miners")).toBeInTheDocument();
    expect(screen.getByText("10 miners")).toBeInTheDocument();
    expect(screen.getByText("7 miners")).toBeInTheDocument();
    expect(screen.getByText("3 miners")).toBeInTheDocument();
  });

  it("renders mdash for all stats when counts are null (loaded but no data)", () => {
    renderWithRouter(
      <FleetHealth
        fleetSize={null}
        healthyMiners={null}
        needsAttentionMiners={null}
        offlineMiners={null}
        sleepingMiners={null}
      />,
    );

    // Should show mdash (\u2014) for each stat, not skeleton bars
    const mdashes = screen.getAllByText("\u2014");
    expect(mdashes).toHaveLength(5); // title + 4 categories

    // No skeleton bars should be present
    expect(screen.queryByTestId("skeleton-bar")).not.toBeInTheDocument();

    // No composition bar or legend should be shown
    expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
  });

  it("renders mdash state when some counts are null", () => {
    renderWithRouter(
      <FleetHealth
        fleetSize={100}
        healthyMiners={null}
        needsAttentionMiners={null}
        offlineMiners={null}
        sleepingMiners={null}
      />,
    );

    // Should show mdash state, not skeleton or data
    const mdashes = screen.getAllByText("\u2014");
    expect(mdashes.length).toBeGreaterThan(0);
    expect(screen.queryByTestId("skeleton-bar")).not.toBeInTheDocument();
  });
});
