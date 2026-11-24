import type { Meta, StoryObj } from "@storybook/react";
import AsicTablePreviewComponent from "./AsicTablePreview";
import { AsicTablePreviewProps } from "./types";
import type { AsicData } from "./types";

const AsicTablePreview = (args: AsicTablePreviewProps) => {
  return (
    <div className="w-[400px]">
      <AsicTablePreviewComponent {...args} className="w-full" />
    </div>
  );
};

const meta = {
  title: "Shared/AsicTablePreview",
  component: AsicTablePreview,
  parameters: {
    layout: "centered",
  },
  tags: ["autodocs"],
} satisfies Meta<typeof AsicTablePreview>;

export default meta;

type Story = StoryObj<typeof meta>;

// Helper to generate mock ASIC data with variety
const generateVariedAsicData = (): AsicData[] => {
  const asics: AsicData[] = [];

  for (let row = 0; row < 6; row++) {
    for (let col = 0; col < 21; col++) {
      let value: number | null;

      // Create variety in the data
      if (row === 0) {
        // First row: gradient from cool to warm
        value = 40 + col * 2;
      } else if (row === 1) {
        // Second row: mostly normal temps with some variation
        value = 55 + Math.sin(col * 0.5) * 10;
      } else if (row === 2) {
        // Third row: warning range
        value = 68 + Math.sin(col * 0.3) * 8;
      } else if (row === 3) {
        // Fourth row: mix of warning and danger
        value = 75 + Math.sin(col * 0.4) * 10;
      } else if (row === 4) {
        // Fifth row: danger to critical range
        value = 82 + col * 0.4;
      } else if (row === 5) {
        // Sixth row: some offline chips mixed with various temps
        if (col % 5 === 0) {
          value = null; // Offline chips
        } else {
          value = 60 + Math.sin(col * 0.6) * 25;
        }
      } else {
        value = 60;
      }

      asics.push({
        row,
        col,
        value: value === null ? null : Math.round(value),
      });
    }
  }

  return asics;
};

// Main story showing variety of temperatures
export const Default: Story = {
  args: {
    asics: generateVariedAsicData(),
    min: 30, // Match the min used in opacity calculation
    warningThreshold: 65, // Default warning threshold
    dangerThreshold: 82, // Default danger threshold
    criticalThreshold: 90, // Default critical threshold
  },
};

// Container module heatmap: a 26x12 grid (CONTAINER_HASHBOARD_COLUMNS x
// CONTAINER_HASHBOARD_ROWS) with taller 16px rows (rowHeight) to reinforce the
// vertical layout on the board card. Temperatures are mostly cool with a few
// warm cells so the ramp reads realistically rather than as a stress test.
const generateContainerAsicData = (): AsicData[] => {
  const cols = 26;
  const rows = 12;
  const asics: AsicData[] = [];

  for (let row = 0; row < rows; row++) {
    for (let col = 0; col < cols; col++) {
      // Gentle warm cluster toward the center; cool everywhere else.
      const rowHeat = 1 - Math.abs(row - rows / 2) / (rows / 2);
      const colHeat = 1 - Math.abs(col - cols / 2) / (cols / 2);
      const value = Math.round(42 + rowHeat * colHeat * 34);
      asics.push({ row, col, value });
    }
  }

  return asics;
};

export const Container: Story = {
  args: {
    asics: generateContainerAsicData(),
    rowHeight: 16, // Taller rows for the container module board card
    min: 30,
    warningThreshold: 65,
    dangerThreshold: 82,
    criticalThreshold: 90,
  },
};
