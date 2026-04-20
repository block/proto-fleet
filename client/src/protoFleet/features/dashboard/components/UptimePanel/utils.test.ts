import { describe, expect, it } from "vitest";
import { generateUptimeHeadline } from "./utils";
import type { SegmentedBarChartData } from "@/protoFleet/features/dashboard/components/SegmentedMetricPanel/types";

describe("generateUptimeHeadline", () => {
  it("returns 'No data' when no data points are provided", () => {
    const result = generateUptimeHeadline([]);
    expect(result).toBe("No data");
  });

  it("returns 'No data' when empty array of arrays is provided", () => {
    const result = generateUptimeHeadline([[]]);
    expect(result).toBe("No data");
  });

  it("returns 'All miners hashing' when all miners are hashing", () => {
    const data: SegmentedBarChartData[][] = [
      [
        {
          datetime: Date.now(),
          hashing: 5,
          notHashing: 0,
        },
      ],
    ];

    const result = generateUptimeHeadline(data);
    expect(result).toBe("All miners hashing");
  });

  it("returns percentage when only one miner is not hashing", () => {
    const data: SegmentedBarChartData[][] = [
      [
        {
          datetime: Date.now(),
          hashing: 4,
          notHashing: 1,
        },
      ],
    ];

    const result = generateUptimeHeadline(data);
    expect(result).toBe("20% not hashing");
  });

  it("returns percentage when multiple miners are not hashing", () => {
    const data: SegmentedBarChartData[][] = [
      [
        {
          datetime: Date.now(),
          hashing: 3,
          notHashing: 2,
        },
      ],
    ];

    const result = generateUptimeHeadline(data);
    expect(result).toBe("40% not hashing");
  });

  it("calculates correct percentage for not hashing miners", () => {
    const data: SegmentedBarChartData[][] = [
      [
        {
          datetime: Date.now(),
          hashing: 97,
          notHashing: 3,
        },
      ],
    ];

    const result = generateUptimeHeadline(data);
    expect(result).toBe("3% not hashing");
  });

  it("rounds percentage to nearest integer", () => {
    const data: SegmentedBarChartData[][] = [
      [
        {
          datetime: Date.now(),
          hashing: 6,
          notHashing: 1,
        },
      ],
    ];

    const result = generateUptimeHeadline(data);
    // 1/7 = 14.28%, should round to 14%
    expect(result).toBe("14% not hashing");
  });

  it("uses the most recent data point from multiple data points", () => {
    const data: SegmentedBarChartData[][] = [
      [
        {
          datetime: Date.now() - 10000,
          hashing: 5,
          notHashing: 0,
        },
        {
          datetime: Date.now() - 5000,
          hashing: 4,
          notHashing: 1,
        },
        {
          datetime: Date.now(),
          hashing: 3,
          notHashing: 2,
        },
      ],
    ];

    const result = generateUptimeHeadline(data);
    expect(result).toBe("40% not hashing");
  });

  it("flattens multi-day data and uses the last point", () => {
    const data: SegmentedBarChartData[][] = [
      [
        {
          datetime: Date.now() - 20000,
          hashing: 5,
          notHashing: 0,
        },
      ],
      [
        {
          datetime: Date.now() - 10000,
          hashing: 4,
          notHashing: 1,
        },
      ],
      [
        {
          datetime: Date.now(),
          hashing: 2,
          notHashing: 3,
        },
      ],
    ];

    const result = generateUptimeHeadline(data);
    expect(result).toBe("60% not hashing");
  });

  it("returns 'No miners' when total count is 0", () => {
    const data: SegmentedBarChartData[][] = [
      [
        {
          datetime: Date.now(),
          hashing: 0,
          notHashing: 0,
        },
      ],
    ];

    const result = generateUptimeHeadline(data);
    expect(result).toBe("No miners");
  });

  it("handles undefined notHashing field", () => {
    const data: SegmentedBarChartData[][] = [
      [
        {
          datetime: Date.now(),
          hashing: 5,
          // notHashing is undefined
        },
      ],
    ];

    const result = generateUptimeHeadline(data);
    expect(result).toBe("All miners hashing");
  });

  it("handles undefined hashing field", () => {
    const data: SegmentedBarChartData[][] = [
      [
        {
          datetime: Date.now(),
          // hashing is undefined
          notHashing: 2,
        },
      ],
    ];

    const result = generateUptimeHeadline(data);
    expect(result).toBe("100% not hashing");
  });

  it("handles 100% not hashing", () => {
    const data: SegmentedBarChartData[][] = [
      [
        {
          datetime: Date.now(),
          hashing: 0,
          notHashing: 5,
        },
      ],
    ];

    const result = generateUptimeHeadline(data);
    expect(result).toBe("100% not hashing");
  });

  it("handles large numbers of miners", () => {
    const data: SegmentedBarChartData[][] = [
      [
        {
          datetime: Date.now(),
          hashing: 950,
          notHashing: 50,
        },
      ],
    ];

    const result = generateUptimeHeadline(data);
    expect(result).toBe("5% not hashing");
  });
});
