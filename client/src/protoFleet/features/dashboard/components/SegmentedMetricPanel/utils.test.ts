import { describe, expect, it } from "vitest";
import type { SegmentConfig, SegmentedBarChartData } from "./types";
import { getCurrentBreakdown } from "./utils";

describe("getCurrentBreakdown", () => {
  const mockSegmentConfig: SegmentConfig = {
    hashing: {
      color: "var(--color-text-primary)",
      label: "Hashing",
      displayInBreakdown: true,
      showButton: false,
      index: 1,
    },
    notHashing: {
      color: "var(--color-core-primary-10)",
      label: "Not hashing",
      displayInBreakdown: true,
      showButton: true,
      buttonVariant: "secondary",
      index: 0,
    },
  };

  it("returns empty array when processedChartData is empty", () => {
    const result = getCurrentBreakdown([], mockSegmentConfig);
    expect(result).toEqual([]);
  });

  it("returns empty array when processedChartData has empty charts", () => {
    const result = getCurrentBreakdown([[]], mockSegmentConfig);
    expect(result).toEqual([]);
  });

  it("calculates breakdown from last bar of single-day chart", () => {
    const processedData: SegmentedBarChartData[][] = [
      [
        {
          datetime: Date.now() - 10000,
          hashing: 5,
          notHashing: 0,
        },
        {
          datetime: Date.now(),
          hashing: 3,
          notHashing: 2,
        },
      ],
    ];

    const result = getCurrentBreakdown(processedData, mockSegmentConfig);

    expect(result).toHaveLength(2);
    expect(result[0]).toMatchObject({
      key: "notHashing",
      label: "Not hashing",
      count: 2,
      percentage: 40,
    });
    expect(result[1]).toMatchObject({
      key: "hashing",
      label: "Hashing",
      count: 3,
      percentage: 60,
    });
  });

  it("calculates breakdown from last bar of last day in multi-day chart", () => {
    const processedData: SegmentedBarChartData[][] = [
      // Day 1
      [
        {
          datetime: Date.now() - 20000,
          hashing: 5,
          notHashing: 0,
        },
      ],
      // Day 2
      [
        {
          datetime: Date.now() - 10000,
          hashing: 4,
          notHashing: 1,
        },
      ],
      // Day 3 (most recent)
      [
        {
          datetime: Date.now(),
          hashing: 2,
          notHashing: 3,
        },
      ],
    ];

    const result = getCurrentBreakdown(processedData, mockSegmentConfig);

    expect(result).toHaveLength(2);
    expect(result[0]).toMatchObject({
      key: "notHashing",
      count: 3,
      percentage: 60,
    });
    expect(result[1]).toMatchObject({
      key: "hashing",
      count: 2,
      percentage: 40,
    });
  });

  it("handles zero total count", () => {
    const processedData: SegmentedBarChartData[][] = [
      [
        {
          datetime: Date.now(),
          hashing: 0,
          notHashing: 0,
        },
      ],
    ];

    const result = getCurrentBreakdown(processedData, mockSegmentConfig);

    expect(result).toHaveLength(2);
    expect(result[0].percentage).toBe(0);
    expect(result[1].percentage).toBe(0);
  });

  it("rounds percentages correctly", () => {
    const processedData: SegmentedBarChartData[][] = [
      [
        {
          datetime: Date.now(),
          hashing: 6,
          notHashing: 1, // 1/7 = 14.28%
        },
      ],
    ];

    const result = getCurrentBreakdown(processedData, mockSegmentConfig);

    const notHashingSegment = result.find((s) => s.key === "notHashing");
    expect(notHashingSegment?.percentage).toBe(14);
  });

  it("handles undefined segment values", () => {
    const processedData: SegmentedBarChartData[][] = [
      [
        {
          datetime: Date.now(),
          hashing: 5,
          // notHashing is undefined
        },
      ],
    ];

    const result = getCurrentBreakdown(processedData, mockSegmentConfig);

    const notHashingSegment = result.find((s) => s.key === "notHashing");
    expect(notHashingSegment?.count).toBe(0);
  });

  it("uses custom percentage label when provided", () => {
    const customConfig: SegmentConfig = {
      ...mockSegmentConfig,
      notHashing: {
        ...mockSegmentConfig.notHashing,
        percentageLabel: "Custom label",
      },
    };

    const processedData: SegmentedBarChartData[][] = [
      [
        {
          datetime: Date.now(),
          hashing: 3,
          notHashing: 2,
        },
      ],
    ];

    const result = getCurrentBreakdown(processedData, customConfig);

    const notHashingSegment = result.find((s) => s.key === "notHashing");
    expect(notHashingSegment?.percentageLabel).toBe("Custom label");
  });

  it("sorts breakdown by index", () => {
    const processedData: SegmentedBarChartData[][] = [
      [
        {
          datetime: Date.now(),
          hashing: 3,
          notHashing: 2,
        },
      ],
    ];

    const result = getCurrentBreakdown(processedData, mockSegmentConfig);

    // notHashing has index 0, hashing has index 1
    expect(result[0].key).toBe("notHashing");
    expect(result[1].key).toBe("hashing");
  });

  it("filters out segments with displayInBreakdown = false", () => {
    const configWithHidden: SegmentConfig = {
      ...mockSegmentConfig,
      hashing: {
        ...mockSegmentConfig.hashing,
        displayInBreakdown: false,
      },
    };

    const processedData: SegmentedBarChartData[][] = [
      [
        {
          datetime: Date.now(),
          hashing: 3,
          notHashing: 2,
        },
      ],
    ];

    const result = getCurrentBreakdown(processedData, configWithHidden);

    expect(result).toHaveLength(1);
    expect(result[0].key).toBe("notHashing");
  });

  describe("Edge case: Legend uses processed chart data, ensuring consistency", () => {
    it("uses the exact data from the last processed chart bar", () => {
      // This test verifies the fix for DASH-1043:
      // Legend should use the same data as the chart's last bar,
      // not independently process raw data which could have newer timestamps

      // Create processed chart data directly (as if from processMultiDayChartData)
      const processedData: SegmentedBarChartData[][] = [
        [
          {
            datetime: Date.now() - 10000,
            hashing: 5,
            notHashing: 0,
          },
          {
            datetime: Date.now() - 5000,
            hashing: 3,
            notHashing: 2, // This is what the chart's last bar shows
          },
        ],
      ];

      // Get breakdown - should use exact values from last bar
      const result = getCurrentBreakdown(processedData, mockSegmentConfig);

      const notHashingSegment = result.find((s) => s.key === "notHashing");
      const hashingSegment = result.find((s) => s.key === "hashing");

      // Verify it matches the last bar exactly
      expect(notHashingSegment?.count).toBe(2);
      expect(hashingSegment?.count).toBe(3);
    });

    it("always matches chart's last bar in multi-day view", () => {
      // Multi-day scenario: Legend should use the last bar of the last day
      const processedData: SegmentedBarChartData[][] = [
        // Day 1
        [
          {
            datetime: Date.now() - 48 * 60 * 60 * 1000,
            hashing: 10,
            notHashing: 0,
          },
        ],
        // Day 2 (most recent day)
        [
          {
            datetime: Date.now() - 12 * 60 * 60 * 1000,
            hashing: 7,
            notHashing: 1,
          },
          {
            datetime: Date.now(), // Last bar of last day
            hashing: 4,
            notHashing: 3,
          },
        ],
      ];

      const result = getCurrentBreakdown(processedData, mockSegmentConfig);

      const notHashingSegment = result.find((s) => s.key === "notHashing");
      const hashingSegment = result.find((s) => s.key === "hashing");

      // Should match the last bar (4 hashing, 3 not hashing)
      // Not day 1 data (10, 0) or first bar of day 2 (7, 1)
      expect(hashingSegment?.count).toBe(4);
      expect(notHashingSegment?.count).toBe(3);
    });

    it("breakdown and chart are guaranteed to be in sync", () => {
      // The key guarantee: since getCurrentBreakdown takes processed chart data,
      // it's IMPOSSIBLE for them to be out of sync
      const processedData: SegmentedBarChartData[][] = [
        [
          {
            datetime: Date.now(),
            hashing: 100,
            notHashing: 50,
          },
        ],
      ];

      // Get the last bar that the chart displays
      const lastChart = processedData[processedData.length - 1];
      const lastBar = lastChart[lastChart.length - 1];

      // Get the breakdown
      const result = getCurrentBreakdown(processedData, mockSegmentConfig);

      // They MUST match because breakdown uses the same processed data
      const notHashingSegment = result.find((s) => s.key === "notHashing");
      const hashingSegment = result.find((s) => s.key === "hashing");

      expect(hashingSegment?.count).toBe(lastBar.hashing);
      expect(notHashingSegment?.count).toBe(lastBar.notHashing);
    });
  });
});
