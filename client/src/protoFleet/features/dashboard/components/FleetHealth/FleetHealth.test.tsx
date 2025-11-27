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
    renderWithRouter(<FleetHealth fleetSize={100} healthyMiners={100} unhealthyMiners={0} offlineMiners={0} />);

    // Check title label
    expect(screen.getByText("Your fleet")).toBeInTheDocument();

    // Check percentages
    expect(screen.getByText("100%")).toBeInTheDocument(); // Healthy

    // Check counts - using getAllByText because "100 miners" appears twice (header and healthy column)
    const healthyCount = screen.getAllByText("100 miners");
    expect(healthyCount).toHaveLength(2); // One in header, one in healthy column

    const zeroMiners = screen.getAllByText("0 miners");
    expect(zeroMiners).toHaveLength(2); // Unhealthy and Offline columns

    // Check that legend is present - using getAllByText since these appear in column headers and legend
    const healthyTexts = screen.getAllByText("Healthy");
    expect(healthyTexts.length).toBeGreaterThan(0); // At least one
    const unhealthyTexts = screen.getAllByText("Unhealthy");
    expect(unhealthyTexts.length).toBeGreaterThan(0); // At least one
    const offlineTexts = screen.getAllByText("Offline");
    expect(offlineTexts.length).toBeGreaterThan(0); // At least one

    // Check CompositionBar is rendered
    const progressBars = screen.getAllByRole("progressbar");
    expect(progressBars.length).toBeGreaterThan(0);
  });

  it("renders correct stats with mixed fleet health", () => {
    renderWithRouter(<FleetHealth fleetSize={200} healthyMiners={178} unhealthyMiners={20} offlineMiners={2} />);

    // Check miner count
    expect(screen.getByText("200 miners")).toBeInTheDocument();

    // Check percentages
    expect(screen.getByText("89%")).toBeInTheDocument(); // Healthy
    expect(screen.getByText("10%")).toBeInTheDocument(); // Unhealthy
    expect(screen.getByText("1%")).toBeInTheDocument(); // Offline

    // Check miner counts
    expect(screen.getByText("178 miners")).toBeInTheDocument();
    expect(screen.getByText("20 miners")).toBeInTheDocument();
    expect(screen.getByText("2 miners")).toBeInTheDocument();
  });

  it("renders stats for fleet with moderate health distribution", () => {
    renderWithRouter(<FleetHealth fleetSize={100} healthyMiners={70} unhealthyMiners={20} offlineMiners={10} />);

    // Check title label
    expect(screen.getByText("Your fleet")).toBeInTheDocument();

    // Check percentages
    expect(screen.getByText("70%")).toBeInTheDocument(); // Healthy
    expect(screen.getByText("20%")).toBeInTheDocument(); // Unhealthy
    expect(screen.getByText("10%")).toBeInTheDocument(); // Offline
  });

  it("renders stats for fleet with critical health distribution", () => {
    renderWithRouter(<FleetHealth fleetSize={100} healthyMiners={30} unhealthyMiners={50} offlineMiners={20} />);

    // Check title label
    expect(screen.getByText("Your fleet")).toBeInTheDocument();

    // Check percentages
    expect(screen.getByText("30%")).toBeInTheDocument(); // Healthy
    expect(screen.getByText("50%")).toBeInTheDocument(); // Unhealthy
    expect(screen.getByText("20%")).toBeInTheDocument(); // Offline
  });

  it("handles division by zero when fleet size is 0", () => {
    renderWithRouter(<FleetHealth fleetSize={0} healthyMiners={0} unhealthyMiners={0} offlineMiners={0} />);

    // Should render without errors
    expect(screen.getByText("Your fleet")).toBeInTheDocument();

    // All percentages should be 0%
    const zeroPercents = screen.getAllByText("0%");
    expect(zeroPercents).toHaveLength(3); // Healthy, Unhealthy, Offline

    // All miner counts should be 0 miners (3 in columns, 1 in header = 4 total)
    const zeroMinerCounts = screen.getAllByText("0 miners");
    expect(zeroMinerCounts).toHaveLength(4);
  });

  it("renders loading state when miner counts are undefined", () => {
    renderWithRouter(<FleetHealth />);

    // Should render skeleton bars instead of values
    expect(screen.getByText("Your fleet")).toBeInTheDocument();

    // Check that all stat labels are present but with skeleton bars
    // Using getAllByText since these appear in both stat headers and legend
    const healthyTexts = screen.getAllByText("Healthy");
    expect(healthyTexts.length).toBeGreaterThan(0);
    const unhealthyTexts = screen.getAllByText("Unhealthy");
    expect(unhealthyTexts.length).toBeGreaterThan(0);
    const offlineTexts = screen.getAllByText("Offline");
    expect(offlineTexts.length).toBeGreaterThan(0);

    // Skeleton bars should be present (4 total - one for fleet health title, one for each segment stat)
    const skeletonBars = screen.getAllByTestId("skeleton-bar");
    expect(skeletonBars.length).toBe(4);
  });

  it("renders partial loading state when some props are undefined", () => {
    renderWithRouter(
      <FleetHealth
        fleetSize={100}
        healthyMiners={70}
        // unhealthyMiners and offlineMiners are undefined
      />,
    );

    // Check title label is present
    expect(screen.getByText("Your fleet")).toBeInTheDocument();

    // Should show defined values for healthy
    expect(screen.getByText("70%")).toBeInTheDocument(); // Healthy percentage
    expect(screen.getByText("70 miners")).toBeInTheDocument(); // Healthy count

    // Undefined values should show skeleton bars (unhealthy and offline)
    const skeletonBars = screen.getAllByTestId("skeleton-bar");
    expect(skeletonBars.length).toBe(2); // Two skeleton bars: unhealthy and offline
  });

  it("renders legend with correct color indicators", () => {
    const { container } = renderWithRouter(
      <FleetHealth fleetSize={100} healthyMiners={85} unhealthyMiners={10} offlineMiners={5} />,
    );

    // Check legend items - using getAllByText since these appear in both column headers and legend
    const healthyTexts = screen.getAllByText("Healthy");
    expect(healthyTexts.length).toBeGreaterThan(0);
    const unhealthyTexts = screen.getAllByText("Unhealthy");
    expect(unhealthyTexts.length).toBeGreaterThan(0);
    const offlineTexts = screen.getAllByText("Offline");
    expect(offlineTexts.length).toBeGreaterThan(0);

    // Check that the triangle SVG exists for unhealthy
    const svgTriangle = container.querySelector("svg");
    expect(svgTriangle).toBeInTheDocument();

    // Check color indicators - there will be multiple instances (in bar and legend)
    const greenIndicators = container.querySelectorAll(".bg-intent-success-fill, .bg-core-primary-fill");
    const redIndicators = container.querySelectorAll(".bg-intent-critical-fill, .fill-intent-critical-fill");
    const grayIndicators = container.querySelectorAll(".bg-grayscale-gray-50, .bg-core-primary-20");

    expect(greenIndicators.length).toBeGreaterThan(0);
    expect(redIndicators.length).toBeGreaterThan(0);
    expect(grayIndicators.length).toBeGreaterThan(0);
  });
});
