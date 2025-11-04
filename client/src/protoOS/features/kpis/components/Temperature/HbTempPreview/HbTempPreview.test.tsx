import { MemoryRouter } from "react-router-dom";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, type Mock, vi } from "vitest";
import HbTempPreview from "./HbTempPreview"; // Adjust the import path as necessary
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";
import {
  useAsicRowsByHbSn,
  useMinerHashboard,
  useMinerHashboardAsics,
  useTemperatureUnit,
} from "@/protoOS/store";
import { type HashboardData } from "@/protoOS/store";

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

vi.mock("@/protoOS/api", () => ({
  useHashboardStatus: vi.fn(),
}));

vi.mock("@/protoOS/store", async (importOriginal) => {
  const actual = (await importOriginal()) as any;
  return {
    ...actual,
    useTemperatureUnit: vi.fn(() => "C"),
    useMinerHashboard: vi.fn(() => ({
      avgAsicTemp: { latest: { value: 60, units: "C" } },
      maxAsicTemp: { latest: { value: 80, units: "C" } },
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
  (useTemperatureUnit as Mock).mockReturnValue("C");
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
          <HbTempPreview
            serial={mockHbData.serial}
            slot={mockHbData.slot ?? 1}
          />
        </MinerHostingProvider>
      </MemoryRouter>,
    );

    expect(screen.getByText("Hashboard 1")).toBeInTheDocument();
    expect(screen.getByTestId("hb-temp-preview")).not.toHaveClass(
      "hover:bg-intent-critical-20",
    );
    expect(screen.getByTestId("asic-table-preview")).toBeInTheDocument();
  });

  it("renders temperature with correct units when temperatureUnit is set to 'F'", () => {
    (useTemperatureUnit as Mock).mockReturnValue("F");

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
          <HbTempPreview serial={hbData.serial} slot={hbData.slot ?? 1} />
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
          <HbTempPreview
            serial={mockHbData.serial}
            slot={mockHbData.slot ?? 1}
          />
        </MinerHostingProvider>
      </MemoryRouter>,
    );

    expect(screen.queryByTestId("asic-table-preview")).not.toBeInTheDocument();
  });

  it("correctly renders overheated state", async () => {
    const overheatedHbData: HashboardData = {
      ...mockHbData,
      temperature: {
        latest: { value: 100, units: "C" },
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
      maxAsicTemp: {
        latest: { value: 100, units: "C" },
      },
    };

    // Mock useMinerHashboard to return overheated data
    (useMinerHashboard as Mock).mockReturnValue(overheatedHbData);

    render(
      <MemoryRouter>
        <MinerHostingProvider>
          <HbTempPreview serial="overheated-serial" slot={1} />
        </MinerHostingProvider>
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("hb-temp-preview")).toHaveClass(
        "hover:bg-intent-critical-20",
      );
    });
  });
});
