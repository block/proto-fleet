import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import Network from "./Network";

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

beforeEach(() => {
  vi.clearAllMocks();
});

describe("Network", () => {
  it("renders page title", () => {
    const { getByText } = render(<Network />);

    expect(getByText("Network")).toBeInTheDocument();
  });

  it("renders network details section", () => {
    const { getByText } = render(<Network />);

    expect(getByText("Network details")).toBeInTheDocument();
    expect(getByText("Subnet mask")).toBeInTheDocument();
    expect(getByText("Gateway")).toBeInTheDocument();
  });

  it("displays network info values", () => {
    const { getByText } = render(<Network />);

    expect(getByText("192.168.1.1")).toBeInTheDocument();
    expect(getByText("192.168.1.0/24")).toBeInTheDocument();
  });
});
