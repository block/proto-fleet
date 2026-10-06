import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import RackCard from "./RackCard";

const baseProps = {
  label: "Rack A",
  cols: 1,
  rows: 1,
  slots: ["healthy" as const],
  statusSegments: [],
  hashrate: "100 TH/s",
  efficiency: "20 J/TH",
  power: "3 kW",
  temperature: "30 °C",
};

describe("RackCard", () => {
  it("shows the telemetry footer by default (full fleet card)", () => {
    render(<RackCard {...baseProps} />);
    expect(screen.getByText("100 TH/s")).toBeInTheDocument();
    expect(screen.getByText("20 J/TH")).toBeInTheDocument();
    expect(screen.getByText("3 kW")).toBeInTheDocument();
    expect(screen.getByText("30 °C")).toBeInTheDocument();
  });

  it("hides the telemetry footer when showMetrics is false (dashboard card)", () => {
    render(<RackCard {...baseProps} showMetrics={false} />);
    expect(screen.getByText("Rack A")).toBeInTheDocument();
    expect(screen.queryByText("100 TH/s")).not.toBeInTheDocument();
    expect(screen.queryByText("20 J/TH")).not.toBeInTheDocument();
    expect(screen.queryByText("3 kW")).not.toBeInTheDocument();
    expect(screen.queryByText("30 °C")).not.toBeInTheDocument();
  });
});
