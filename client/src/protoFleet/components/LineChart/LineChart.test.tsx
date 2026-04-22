import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import LineChart from "./LineChart";

const { sharedLineChartSpy } = vi.hoisted(() => ({
  sharedLineChartSpy: vi.fn((_props: unknown) => <div data-testid="shared-line-chart" />),
}));

vi.mock("@/shared/components/LineChart", () => ({
  default: sharedLineChartSpy,
}));

describe("ProtoFleet LineChart", () => {
  it("opts into simplified aggregate-only tooltip behavior", () => {
    render(<LineChart chartData={[{ datetime: 1_700_000_000_000, total: 54 }]} aggregateKey="total" />);

    expect(screen.getByTestId("shared-line-chart")).toBeInTheDocument();
    expect(sharedLineChartSpy).toHaveBeenCalled();
    expect(sharedLineChartSpy.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({
        aggregateKey: "total",
        hideAggregateContextWhenSingleSeries: true,
      }),
    );
  });
});
