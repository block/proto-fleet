import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import Cooling from "./Cooling";
import { useCoolingStatus } from "@/protoOS/api";
import { useCoolingMode, useFansTelemetry, useIsSleeping } from "@/protoOS/store";

vi.mock("@/protoOS/api", () => ({
  useCoolingStatus: vi.fn(),
}));

vi.mock("@/protoOS/store", () => ({
  useCoolingMode: vi.fn(),
  useFansTelemetry: vi.fn(),
  useIsSleeping: vi.fn(),
}));

vi.mock("@/protoOS/components/Power", () => ({
  EnteringSleepDialog: () => null,
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(() => 1),
  updateToast: vi.fn(),
}));

describe("Cooling", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(useCoolingStatus).mockReturnValue({
      data: undefined,
      error: undefined,
      loaded: true,
      pending: false,
      setCooling: vi.fn(),
    } as ReturnType<typeof useCoolingStatus>);
    vi.mocked(useFansTelemetry).mockReturnValue([]);
    vi.mocked(useIsSleeping).mockReturnValue(false);
  });

  test("selects air cooled when the store already has Auto mode on mount", () => {
    vi.mocked(useCoolingMode).mockReturnValue("Auto");

    render(<Cooling />);

    expect(screen.getByTestId("cooling-option-air").querySelector("input")).toBeChecked();
    expect(screen.getByTestId("cooling-option-immersion").querySelector("input")).not.toBeChecked();
  });

  test("selects immersion cooled when the store already has Off mode on mount", () => {
    vi.mocked(useCoolingMode).mockReturnValue("Off");

    render(<Cooling />);

    expect(screen.getByTestId("cooling-option-air").querySelector("input")).not.toBeChecked();
    expect(screen.getByTestId("cooling-option-immersion").querySelector("input")).toBeChecked();
  });
});
