import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import ContainerSystemSchematic, { type ContainerSystemSchematicProps } from "./ContainerSystemSchematic";

const props: ContainerSystemSchematicProps = {
  tankBanks: [
    {
      id: "Tanks 1–3",
      temperature: "65.5°C",
      pressure: "14.4 psi",
      tanks: [1, 2, 3].map((label) => ({ id: `tank-${label}`, label: `${label}`, levelPercent: 82 })),
    },
    {
      id: "Tanks 4–6",
      temperature: "—",
      pressure: "—",
      tanks: [4, 5, 6].map((label) => ({ id: `tank-${label}`, label: `${label}`, levelPercent: null })),
    },
  ],
  fans: Array.from({ length: 10 }, (_, index) => ({
    id: `fan-${index + 1}`,
    label: `${index + 1}`,
    speedPercent: index === 9 ? 12 : 80,
    status: index === 9 ? "critical" : "normal",
  })),
  flowRate: "991 GPM",
  flowAlert: true,
  pumpSpeed: "30 Hz",
  dryCoolerInletTemperature: "65.5°C",
  dryCoolerOutletPressure: "11.6 psi",
  pumpOutletPressure: "13.5 psi",
};

describe("ContainerSystemSchematic", () => {
  it("announces the live equipment readings represented by the schematic", () => {
    render(<ContainerSystemSchematic {...props} />);

    expect(screen.getByRole("region", { name: "Container cooling system schematic" })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "1: 82% full" })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "4: level unavailable" })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "10: 12%, critical" })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Flow rate: 991 GPM, alert" })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Pump speed: 30 Hz" })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Dry cooler outlet pressure: 11.6 psi" })).toBeInTheDocument();
  });

  it("keeps fractional live fan readings legible inside their gauges", () => {
    render(<ContainerSystemSchematic {...props} fans={[{ id: "fan-1", label: "1", speedPercent: 80.123456 }]} />);
    expect(screen.getByRole("img", { name: "1: 80%" })).toBeInTheDocument();
    expect(screen.getByText("80%")).toBeInTheDocument();
  });
});
