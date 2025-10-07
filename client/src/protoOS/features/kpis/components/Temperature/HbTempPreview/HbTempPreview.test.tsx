import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, type Mock, vi } from "vitest";
import HbTempPreview from "./HbTempPreview"; // Adjust the import path as necessary
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";
import { useAsicRowsByHbSn, useMinerHashboardAsics } from "@/protoOS/store";
import { type HashboardData } from "@/protoOS/store";
import { TEMP_UNITS, usePreferences } from "@/shared/features/preferences/";

const mockHbData: HashboardData = {
  serial: "1234567890",
  slot: 1,
  bay: 1,
  asicIds: [],
  temperature: {
    timeSeries: {
      units: "C",
      values: [40, 60, 80],
      aggregates: {
        avg: { value: 60, units: "C" },
        max: { value: 80, units: "C" },
        min: { value: 40, units: "C" },
      },
      startTime: 1234567890000,
      endTime: 1234567892000,
    },
  },
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

vi.mock("@/protoOS/api", () => ({
  useHashboardStatus: vi.fn(),
}));

vi.mock("@/protoOS/store", async (importOriginal) => {
  const actual = (await importOriginal()) as any;
  return {
    ...actual,
    useMinerHashboard: vi.fn(() => ({
      avgAsicTemp: { value: 60, units: "C" },
      maxAsicTemp: { value: 80, units: "C" },
    })),
    useMinerHashboardAsics: vi.fn(() => []), // Default to empty asics array
    useAsicRowsByHbSn: vi.fn(() => []),
  };
});

// Let AsicTablePreview use real logic to decide when to show spinner vs table
vi.mock("./AsicCell", () => ({
  default: ({ asic }: { asic: any }) => (
    <div data-testid="asic-cell">Asic {asic.id}</div>
  ),
}));

beforeEach(() => {
  (usePreferences as Mock).mockReturnValue({
    temperatureUnits: "celsius",
  });
  // Reset asic mocks to default (empty) state
  (useMinerHashboardAsics as Mock).mockReturnValue([]);
  (useAsicRowsByHbSn as Mock).mockReturnValue([]);
});

describe("HbTempPreview", () => {
  it("renders the component with correct initial state", () => {
    // Override the mock to return some asic data for this test
    (useMinerHashboardAsics as Mock).mockReturnValue([
      { id: 1, row: 0, column: 0, temp_c: 60 },
      { id: 2, row: 0, column: 1, temp_c: 65 },
    ]);
    (useAsicRowsByHbSn as Mock).mockReturnValue([0]); // Return row 0

    render(
      <MemoryRouter>
        <MinerHostingProvider>
          <HbTempPreview hbData={mockHbData} />
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

    const hbData: HashboardData = {
      serial: "12345",
      slot: 1,
      bay: 1,
      asicIds: [],
      temperature: {
        timeSeries: {
          units: "C",
          values: [55],
          aggregates: {
            avg: { value: 50, units: "C" },
            max: { value: 60, units: "C" },
            min: { value: 40, units: "C" },
          },
          startTime: 1234567890000,
          endTime: 1234567892000,
        },
      },
    };

    render(
      <MemoryRouter>
        <MinerHostingProvider>
          <HbTempPreview hbData={hbData} />
        </MinerHostingProvider>
      </MemoryRouter>,
    );

    // Verify that the component renders with Fahrenheit units - using the actual converted values from the mock
    expect(screen.getByText("140.0º F")).toBeInTheDocument(); // Mock avgAsicTemp: 60°C -> 140°F
    expect(screen.getByText("176.0º F")).toBeInTheDocument(); // Mock maxAsicTemp: 80°C -> 176°F
  });

  it("renders spinner if there is no asic data", () => {
    render(
      <MemoryRouter>
        <MinerHostingProvider>
          <HbTempPreview hbData={mockHbData} />
        </MinerHostingProvider>
      </MemoryRouter>,
    );

    expect(screen.queryByTestId("asic-table-preview")).not.toBeInTheDocument();
  });

  it("correctly renders overheated state", () => {
    const overheatedHbData: HashboardData = {
      ...mockHbData,
      temperature: {
        timeSeries: {
          units: "C",
          values: [40, 60, 100], // Overheated
          aggregates: {
            avg: { value: 60, units: "C" },
            max: { value: 100, units: "C" },
            min: { value: 40, units: "C" },
          },
          startTime: 1234567890000,
          endTime: 1234567892000,
        },
      },
    };

    render(
      <MemoryRouter>
        <MinerHostingProvider>
          <HbTempPreview hbData={overheatedHbData} />
        </MinerHostingProvider>
      </MemoryRouter>,
    );

    expect(screen.getByTestId("hb-temp-preview")).toHaveClass(
      "hover:bg-intent-critical-20",
    );
  });
});
