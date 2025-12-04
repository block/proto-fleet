import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import General from "./General";

vi.mock("@/protoFleet/api/useNetworkInfo", () => ({
  useNetworkInfo: vi.fn(() => ({
    data: {
      gateway: "192.168.1.1",
      subnet: "192.168.1.0/24",
    },
  })),
}));

vi.mock("@/protoFleet/store", () => ({
  useTheme: vi.fn(() => "system"),
  useSetTheme: vi.fn(() => vi.fn()),
  useTemperatureUnit: vi.fn(() => "C"),
  useSetTemperatureUnit: vi.fn(() => vi.fn()),
}));

vi.mock("@/shared/utils/version", () => ({
  buildVersionInfo: {
    version: "v1.2.3",
    buildDate: "2025-01-01",
    commit: "abc123",
  },
}));

beforeEach(() => {
  vi.clearAllMocks();
});

describe("General", () => {
  it("renders page title", () => {
    const { getByText } = render(<General />);

    expect(getByText("General")).toBeInTheDocument();
  });

  it("renders software version", () => {
    const { getByText } = render(<General />);

    expect(getByText("Proto Fleet v1.2.3")).toBeInTheDocument();
  });

  it("renders network details section", () => {
    const { getByText } = render(<General />);

    expect(getByText("Network details")).toBeInTheDocument();
    expect(getByText("Subnet mask")).toBeInTheDocument();
    expect(getByText("Gateway")).toBeInTheDocument();
  });

  it("renders preferences section", () => {
    const { getByText } = render(<General />);

    expect(getByText("Preferences")).toBeInTheDocument();
    expect(getByText("Theme")).toBeInTheDocument();
    expect(getByText("Temperature")).toBeInTheDocument();
  });

  it("displays network info values", () => {
    const { getByText } = render(<General />);

    expect(getByText("192.168.1.1")).toBeInTheDocument();
    expect(getByText("192.168.1.0/24")).toBeInTheDocument();
  });
});
