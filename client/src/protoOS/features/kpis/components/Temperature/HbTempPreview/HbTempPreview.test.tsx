import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, type Mock, vi } from "vitest";
import { HbTemperature } from "@/protoOS/features/kpis/hooks";
import HbTempPreview from "./HbTempPreview"; // Adjust the import path as necessary
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";
import { TEMP_UNITS, usePreferences } from "@/shared/features/preferences/";
import { getDisplayValue } from "@/shared/utils/stringUtils";
import { convertCtoF } from "@/shared/utils/utility";

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
  slot: 1,
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

vi.mock("@/shared/features/preferences/", async () => {
  const actual = await vi.importActual("@/shared/features/preferences/");
  return {
    ...actual,
    usePreferences: vi.fn(() => ({
      temperatureUnits: "celsius",
      theme: "light", // Mock theme
      setTheme: vi.fn(), // Mock setTheme function
    })),
  };
});

beforeEach(() => {
  (usePreferences as Mock).mockReturnValue({
    temperatureUnits: "celsius",
  });
});

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
    expect(screen.getByTestId("hb-temp-preview")).not.toHaveClass(
      "hover:bg-intent-critical-20",
    );
    expect(screen.getByTestId("asic-table-preview")).toBeInTheDocument();
  });

  it("renders temperature with correct units when temperatureUnits is set to 'fahrenheit'", () => {
    (usePreferences as Mock).mockReturnValue({
      temperatureUnits: TEMP_UNITS.fahrenheit,
    });

    const hbData = {
      name: "Hashboard 1",
      serial: "12345",
      aggregates: {
        avg: 50,
        max: 60,
        min: 40,
      },
      data: [{ value: 55 }],
      slot: 1,
    };

    render(
      <MemoryRouter>
        <MinerHostingProvider>
          <HbTempPreview hbData={hbData} />
        </MinerHostingProvider>
      </MemoryRouter>,
    );

    // Verify that the stats render with Fahrenheit units
    const temp = screen.getByText(
      `${getDisplayValue(convertCtoF(55)) + " ºF"}`,
    );
    expect(temp).toBeInTheDocument();
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
  });
});
