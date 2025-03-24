import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { HbTemperature } from "../../../hooks";
import HbTempPreview from "./HbTempPreview"; // Adjust the import path as necessary
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";

const mockHbData: HbTemperature = {
  name: "Hashboard 1",
  serial: "1234567890",
  aggregates: {
    avg: 60,
    max: 80,
    min: 40,
  },
  data: [
    { datetime: 1234567890, value: 40 },
    { datetime: 1234567891, value: 60 },
    { datetime: 1234567892, value: 80 },
  ],
};

const createMockAsics = (temp = 80) => {
  return Array(100)
    .fill(0)
    .map((_, index) => ({
      id: index,
      row: Math.floor(index / 10),
      column: index % 10,
      freq_mhz: 800,
      temp_c: temp,
      hashrate_ghs: 756.62,
      ideal_hashrate_ghs: 251.73,
      error_rate: 0.49,
    }));
};

describe("HbTempPreview", () => {
  it("renders the component with correct initial state", () => {
    render(
      <MemoryRouter>
        <MinerHostingProvider>
          <HbTempPreview hbData={mockHbData} asics={createMockAsics()} />
        </MinerHostingProvider>
      </MemoryRouter>,
    );

    expect(screen.getByText("Hashboard 1")).toBeInTheDocument();
    expect(screen.getByText("Average")).toBeInTheDocument();
    expect(screen.getByText("Highest")).toBeInTheDocument();
    expect(screen.getByText("Lowest")).toBeInTheDocument();
    expect(screen.getByTestId("hb-temp-preview")).not.toHaveClass(
      "hover:bg-intent-critical-20",
    );
    expect(screen.getByTestId("asic-table-preview")).toBeInTheDocument();
  });

  it("renders spinner if there is no asic data", () => {
    render(
      <MemoryRouter>
        <MinerHostingProvider>
          <HbTempPreview hbData={mockHbData} asics={undefined} />
        </MinerHostingProvider>
      </MemoryRouter>,
    );

    expect(screen.queryByTestId("asic-table-preview")).not.toBeInTheDocument();
  });

  it("correctly renders overheated state", () => {
    const overheatedHbData = {
      ...mockHbData,
      data: [
        { datetime: 1234567890, value: 40 },
        { datetime: 1234567891, value: 60 },
        { datetime: 1234567892, value: 100 }, // Overheated
      ],
    };

    render(
      <MemoryRouter>
        <MinerHostingProvider>
          <HbTempPreview hbData={overheatedHbData} asics={undefined} />
        </MinerHostingProvider>
      </MemoryRouter>,
    );

    expect(screen.getByTestId("hb-temp-preview")).toHaveClass(
      "hover:bg-intent-critical-20",
    );
    expect(screen.getByText("Overheating")).toBeInTheDocument();
  });
});
