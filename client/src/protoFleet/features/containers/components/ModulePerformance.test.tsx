import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import type { useModuleHistory } from "../hooks/useModuleHistory";
import ModulePerformance from "./ModulePerformance";
import { ModuleSampleSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

const mocks = vi.hoisted(() => ({ history: vi.fn<typeof useModuleHistory>(), setDuration: vi.fn() }));
vi.mock("../hooks/useModuleHistory", () => ({ useModuleHistory: mocks.history }));
vi.mock("@/protoFleet/store", () => ({
  useDuration: () => "24h",
  useSetDuration: () => mocks.setDuration,
  useTemperatureUnit: () => "C",
}));
// Chart geometry requires a browser; the real widget and data transformation remain under test.
vi.mock("@/protoFleet/components/LineChart", () => ({
  default: ({ units }: { units: string }) => <div aria-label={`Chart (${units})`} />,
}));

beforeEach(() => {
  vi.clearAllMocks();
  mocks.history.mockReturnValue({ samples: [], endTime: Date.now(), error: undefined, retry: vi.fn() });
});

it("offers retry after failure without claiming the period has no readings", () => {
  const result = { samples: [], endTime: undefined, error: "Network unavailable", retry: vi.fn() };
  mocks.history.mockReturnValue(result);
  render(<ModulePerformance moduleId={91n} enabled />);
  expect(screen.getByRole("alert")).toHaveTextContent("Network unavailable");
  expect(screen.queryAllByText("No readings recorded in this period.")).toHaveLength(0);
  expect(screen.queryAllByText("No data")).toHaveLength(0);
  fireEvent.click(screen.getByRole("button", { name: "Retry history" }));
  expect(result.retry).toHaveBeenCalledOnce();
  fireEvent.click(screen.getByRole("button", { name: "1h" }));
  expect(mocks.setDuration).toHaveBeenCalledWith("1h");
});

it("explains disabled access without claiming history is empty", () => {
  render(<ModulePerformance moduleId={91n} enabled={false} />);
  expect(screen.getByRole("status")).toHaveTextContent("History unavailable until module access can be refreshed.");
  expect(screen.queryAllByText("No readings recorded in this period.")).toHaveLength(0);
  expect(screen.queryAllByText("No data")).toHaveLength(0);
  expect(screen.queryByRole("button", { name: "Retry history" })).not.toBeInTheDocument();
});

it("shows loading placeholders before history arrives", () => {
  mocks.history.mockReturnValue({ samples: undefined, endTime: undefined, error: undefined, retry: vi.fn() });
  render(<ModulePerformance moduleId={91n} enabled />);
  expect(screen.getAllByTestId("skeleton-bar").length).toBeGreaterThanOrEqual(4);
  expect(screen.queryAllByText("No readings recorded in this period.")).toHaveLength(0);
});

it("shows empty charts after a successful response with no readings", () => {
  render(<ModulePerformance moduleId={91n} enabled />);
  expect(screen.getAllByText("No data")).toHaveLength(4);
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});

it("keeps available metrics visible when other metrics have no readings", () => {
  const endTime = Date.now();
  mocks.history.mockReturnValue({
    samples: [create(ModuleSampleSchema, { observedAt: timestampFromMs(endTime - 90_000), hashrateHs: 200e12 })],
    endTime,
    error: undefined,
    retry: vi.fn(),
  });
  render(<ModulePerformance moduleId={91n} enabled />);
  expect(screen.getByLabelText("Chart (TH/s)")).toBeInTheDocument();
  expect(screen.getAllByText("No data")).toHaveLength(3);
});
