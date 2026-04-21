import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import HashboardSelector from "./HashboardSelector";

// Mock the store
const mockGetHashboard = vi.fn();
vi.mock("@/protoOS/store", () => ({
  useMinerStore: {
    getState: () => ({
      hardware: {
        getHashboard: mockGetHashboard,
      },
    }),
  },
}));

// Mock the color utility
vi.mock("@/protoOS/features/kpis/utility", () => ({
  getHashboardColor: (slot: number) => `--color-hashboard-${slot}`,
}));

// Mock the CSS variable hook
vi.mock("@/shared/hooks/useCssVariable", () => ({
  default: (colorVariable: string) => colorVariable,
}));

describe("HashboardSelector", () => {
  const aggregateKey = "miner-total";
  const mockSetActiveChartLines = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    mockGetHashboard.mockImplementation((serial: string) => {
      const slotMap: Record<string, number> = {
        "hashboard-1": 1,
        "hashboard-2": 2,
        "hashboard-3": 3,
      };
      return { slot: slotMap[serial] };
    });
  });

  describe("Default State (No Filters Active)", () => {
    it("renders Summary button as unselected when no filters are active", () => {
      render(
        <HashboardSelector
          chartLines={[aggregateKey, "hashboard-1", "hashboard-2"]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={[]}
          aggregateKey={aggregateKey}
        />,
      );

      const summaryButton = screen.getByText("Summary");
      expect(summaryButton).toBeInTheDocument();
      // Button should have ghost variant (unselected) styling
      expect(summaryButton.closest("button")).toHaveClass("border-transparent");
    });

    it("renders All Hashboards button as unselected when no filters are active", () => {
      render(
        <HashboardSelector
          chartLines={[aggregateKey, "hashboard-1", "hashboard-2"]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={[]}
          aggregateKey={aggregateKey}
        />,
      );

      const allHashboardsButton = screen.getByText("All Hashboards");
      expect(allHashboardsButton).toBeInTheDocument();
      expect(allHashboardsButton.closest("button")).toHaveClass("border-transparent");
    });

    it("renders individual hashboard buttons as unselected when no filters are active", () => {
      render(
        <HashboardSelector
          chartLines={[aggregateKey, "hashboard-1", "hashboard-2", "hashboard-3"]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={[]}
          aggregateKey={aggregateKey}
        />,
      );

      const hashboard1Button = screen.getByText("1");
      const hashboard2Button = screen.getByText("2");
      const hashboard3Button = screen.getByText("3");

      expect(hashboard1Button.closest("button")).toHaveClass("border-transparent");
      expect(hashboard2Button.closest("button")).toHaveClass("border-transparent");
      expect(hashboard3Button.closest("button")).toHaveClass("border-transparent");
    });
  });

  describe("Entering Filtered Mode", () => {
    it("enters filtered mode when Summary button is clicked from default state", async () => {
      const user = userEvent.setup();

      render(
        <HashboardSelector
          chartLines={[aggregateKey, "hashboard-1", "hashboard-2"]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={[]}
          aggregateKey={aggregateKey}
        />,
      );

      const summaryButton = screen.getByText("Summary");
      await user.click(summaryButton);

      expect(mockSetActiveChartLines).toHaveBeenCalledWith([aggregateKey]);
    });

    it("enters filtered mode when a hashboard button is clicked from default state", async () => {
      const user = userEvent.setup();

      render(
        <HashboardSelector
          chartLines={[aggregateKey, "hashboard-1", "hashboard-2"]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={[]}
          aggregateKey={aggregateKey}
        />,
      );

      const hashboard1Button = screen.getByText("1");
      await user.click(hashboard1Button);

      expect(mockSetActiveChartLines).toHaveBeenCalledWith(["hashboard-1"]);
    });

    it("enters filtered mode when All Hashboards button is clicked from default state", async () => {
      const user = userEvent.setup();

      render(
        <HashboardSelector
          chartLines={[aggregateKey, "hashboard-1", "hashboard-2", "hashboard-3"]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={[]}
          aggregateKey={aggregateKey}
        />,
      );

      const allHashboardsButton = screen.getByText("All Hashboards");
      await user.click(allHashboardsButton);

      expect(mockSetActiveChartLines).toHaveBeenCalledWith(["hashboard-1", "hashboard-2", "hashboard-3"]);
    });
  });

  describe("Toggle Behavior in Filtered Mode", () => {
    it("toggles Summary on when clicked in filtered mode", async () => {
      const user = userEvent.setup();

      render(
        <HashboardSelector
          chartLines={[aggregateKey, "hashboard-1", "hashboard-2"]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={["hashboard-1"]}
          aggregateKey={aggregateKey}
        />,
      );

      const summaryButton = screen.getByText("Summary");
      await user.click(summaryButton);

      expect(mockSetActiveChartLines).toHaveBeenCalledWith(["hashboard-1", aggregateKey]);
    });

    it("toggles hashboard on when clicked in filtered mode", async () => {
      const user = userEvent.setup();

      render(
        <HashboardSelector
          chartLines={[aggregateKey, "hashboard-1", "hashboard-2"]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={["hashboard-1"]}
          aggregateKey={aggregateKey}
        />,
      );

      const hashboard2Button = screen.getByText("2");
      await user.click(hashboard2Button);

      expect(mockSetActiveChartLines).toHaveBeenCalledWith(["hashboard-1", "hashboard-2"]);
    });

    it("toggles hashboard off when clicked and it's already selected", async () => {
      const user = userEvent.setup();

      render(
        <HashboardSelector
          chartLines={[aggregateKey, "hashboard-1", "hashboard-2"]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={["hashboard-1", "hashboard-2"]}
          aggregateKey={aggregateKey}
        />,
      );

      const hashboard1Button = screen.getByText("1");
      await user.click(hashboard1Button);

      expect(mockSetActiveChartLines).toHaveBeenCalledWith(["hashboard-2"]);
    });
  });

  describe("Returning to Default State", () => {
    it("returns to default (empty array) when last filter is removed", async () => {
      const user = userEvent.setup();

      render(
        <HashboardSelector
          chartLines={[aggregateKey, "hashboard-1", "hashboard-2"]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={["hashboard-1"]}
          aggregateKey={aggregateKey}
        />,
      );

      const hashboard1Button = screen.getByText("1");
      await user.click(hashboard1Button);

      expect(mockSetActiveChartLines).toHaveBeenCalledWith([]);
    });

    it("returns to default when Summary is the last filter and is removed", async () => {
      const user = userEvent.setup();

      render(
        <HashboardSelector
          chartLines={[aggregateKey, "hashboard-1", "hashboard-2"]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={[aggregateKey]}
          aggregateKey={aggregateKey}
        />,
      );

      const summaryButton = screen.getByText("Summary");
      await user.click(summaryButton);

      expect(mockSetActiveChartLines).toHaveBeenCalledWith([]);
    });
  });

  describe("All Hashboards Button Behavior", () => {
    it("selects all hashboards when some are not selected", async () => {
      const user = userEvent.setup();

      render(
        <HashboardSelector
          chartLines={[aggregateKey, "hashboard-1", "hashboard-2", "hashboard-3"]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={["hashboard-1"]}
          aggregateKey={aggregateKey}
        />,
      );

      const allHashboardsButton = screen.getByText("All Hashboards");
      await user.click(allHashboardsButton);

      expect(mockSetActiveChartLines).toHaveBeenCalledWith(["hashboard-1", "hashboard-2", "hashboard-3"]);
    });

    it("deselects all hashboards when all are selected", async () => {
      const user = userEvent.setup();

      render(
        <HashboardSelector
          chartLines={[aggregateKey, "hashboard-1", "hashboard-2", "hashboard-3"]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={["hashboard-1", "hashboard-2", "hashboard-3"]}
          aggregateKey={aggregateKey}
        />,
      );

      const allHashboardsButton = screen.getByText("All Hashboards");
      await user.click(allHashboardsButton);

      // Should return to default (empty array) since no filters remain
      expect(mockSetActiveChartLines).toHaveBeenCalledWith([]);
    });

    it("preserves Summary state when selecting all hashboards", async () => {
      const user = userEvent.setup();

      render(
        <HashboardSelector
          chartLines={[aggregateKey, "hashboard-1", "hashboard-2", "hashboard-3"]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={[aggregateKey, "hashboard-1"]}
          aggregateKey={aggregateKey}
        />,
      );

      const allHashboardsButton = screen.getByText("All Hashboards");
      await user.click(allHashboardsButton);

      expect(mockSetActiveChartLines).toHaveBeenCalledWith([aggregateKey, "hashboard-1", "hashboard-2", "hashboard-3"]);
    });

    it("preserves Summary state when deselecting all hashboards", async () => {
      const user = userEvent.setup();

      render(
        <HashboardSelector
          chartLines={[aggregateKey, "hashboard-1", "hashboard-2", "hashboard-3"]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={[aggregateKey, "hashboard-1", "hashboard-2", "hashboard-3"]}
          aggregateKey={aggregateKey}
        />,
      );

      const allHashboardsButton = screen.getByText("All Hashboards");
      await user.click(allHashboardsButton);

      // Should keep only Summary
      expect(mockSetActiveChartLines).toHaveBeenCalledWith([aggregateKey]);
    });

    it("returns to default when deselecting all hashboards and Summary is not selected", async () => {
      const user = userEvent.setup();

      render(
        <HashboardSelector
          chartLines={[aggregateKey, "hashboard-1", "hashboard-2", "hashboard-3"]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={["hashboard-1", "hashboard-2", "hashboard-3"]}
          aggregateKey={aggregateKey}
        />,
      );

      const allHashboardsButton = screen.getByText("All Hashboards");
      await user.click(allHashboardsButton);

      expect(mockSetActiveChartLines).toHaveBeenCalledWith([]);
    });
  });

  describe("Button Visual States", () => {
    it("shows Summary button as selected in filtered mode when Summary is active", () => {
      render(
        <HashboardSelector
          chartLines={[aggregateKey, "hashboard-1", "hashboard-2"]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={[aggregateKey]}
          aggregateKey={aggregateKey}
        />,
      );

      const summaryButton = screen.getByText("Summary");
      expect(summaryButton.closest("button")).toHaveClass("border-core-primary-fill");
    });

    it("shows All Hashboards button as selected when all hashboards are active", () => {
      render(
        <HashboardSelector
          chartLines={[aggregateKey, "hashboard-1", "hashboard-2", "hashboard-3"]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={["hashboard-1", "hashboard-2", "hashboard-3"]}
          aggregateKey={aggregateKey}
        />,
      );

      const allHashboardsButton = screen.getByText("All Hashboards");
      expect(allHashboardsButton.closest("button")).toHaveClass("border-core-primary-fill");
    });

    it("shows individual hashboard button as selected when active in filtered mode", () => {
      render(
        <HashboardSelector
          chartLines={[aggregateKey, "hashboard-1", "hashboard-2"]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={["hashboard-1"]}
          aggregateKey={aggregateKey}
        />,
      );

      const hashboard1Button = screen.getByText("1");
      const hashboard2Button = screen.getByText("2");

      expect(hashboard1Button.closest("button")).toHaveClass("border-core-primary-fill");
      expect(hashboard2Button.closest("button")).toHaveClass("border-transparent");
    });
  });

  describe("Edge Cases", () => {
    it("does not render All Hashboards button when there are no hashboard lines", () => {
      render(
        <HashboardSelector
          chartLines={[aggregateKey]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={[]}
          aggregateKey={aggregateKey}
        />,
      );

      expect(screen.queryByText("All Hashboards")).not.toBeInTheDocument();
    });

    it("handles empty chartLines array", () => {
      render(
        <HashboardSelector
          chartLines={[]}
          setActiveChartLines={mockSetActiveChartLines}
          activeChartLines={[]}
          aggregateKey={aggregateKey}
        />,
      );

      expect(screen.getByText("Summary")).toBeInTheDocument();
      expect(screen.queryByText("All Hashboards")).not.toBeInTheDocument();
    });
  });
});
