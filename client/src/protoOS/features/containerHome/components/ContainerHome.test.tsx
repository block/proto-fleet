import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import ContainerHomeToolbar from "./ContainerHomeToolbar";
import ContainerMetricView from "./ContainerMetricView";
import { LocationBarChart } from "./HomeCharts";

describe("container Home presentation", () => {
  it("keeps embedded navigation and canonical duration values", () => {
    const onDurationChange = vi.fn();
    render(
      <MemoryRouter>
        <ContainerHomeToolbar
          tab="power"
          duration="12h"
          onDurationChange={onDurationChange}
          links={{
            overview: "/miners/ct1/",
            hashrate: "/miners/ct1/hashrate",
            efficiency: "/miners/ct1/efficiency",
            power: "/miners/ct1/power-usage",
            temperature: "/miners/ct1/temperature",
            system: "/miners/ct1/?view=system",
          }}
        />
      </MemoryRouter>,
    );
    expect(screen.getByRole("link", { name: "Power" })).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: "Power" })).toHaveAttribute("href", "/miners/ct1/power-usage");
    fireEvent.click(screen.getByRole("button", { name: "48h" }));
    expect(onDurationChange).toHaveBeenCalledWith("48h");
  });

  it("renders zero separately from missing data and controls mode selection", () => {
    const onModeChange = vi.fn();
    render(
      <ContainerMetricView
        title="Power"
        readings={[
          { id: "pdu", label: "PDU A", value: "0.0 kW" },
          { id: "aux", label: "Aux", value: "—" },
        ]}
        history={{ series: [], units: "kW", window: [0, 1] }}
        mode="system"
        modes={[
          { id: "system", label: "System" },
          { id: "fluid", label: "Fluid" },
        ]}
        onModeChange={onModeChange}
      />,
    );
    expect(within(screen.getByRole("region", { name: "Live readings" })).getByText("0.0 kW")).toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.getByText("History unavailable for this selection.")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Fluid" }));
    expect(onModeChange).toHaveBeenCalledWith("fluid");
    expect(screen.getByRole("button", { name: "System" })).toHaveAttribute("aria-pressed", "true");
  });

  it("renders reported locations without inventing six tanks", () => {
    render(<LocationBarChart readings={[{ id: "tank-7", label: "Tank West", amount: null, value: "—" }]} />);
    expect(screen.getAllByRole("listitem")).toHaveLength(1);
    expect(screen.getByText("Tank West")).toBeVisible();
    expect(screen.getByText("—")).toBeVisible();
  });
});
