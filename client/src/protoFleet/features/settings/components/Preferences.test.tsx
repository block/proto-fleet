import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import Preferences from "./Preferences";

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

describe("Preferences", () => {
  it("renders page title", () => {
    const { getByText } = render(<Preferences />);

    expect(getByText("Preferences")).toBeInTheDocument();
  });

  it("renders display preferences", () => {
    const { getByText } = render(<Preferences />);

    expect(getByText("Display")).toBeInTheDocument();
    expect(getByText("Theme")).toBeInTheDocument();
    expect(getByText("Temperature")).toBeInTheDocument();
  });

  it("renders software version", () => {
    const { getByText } = render(<Preferences />);

    expect(getByText("Proto Fleet v1.2.3")).toBeInTheDocument();
  });
});
