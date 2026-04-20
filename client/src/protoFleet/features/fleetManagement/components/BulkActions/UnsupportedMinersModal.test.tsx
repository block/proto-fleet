import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import UnsupportedMinersModal from "./UnsupportedMinersModal";
import type { UnsupportedMinerGroup } from "@/protoFleet/api/generated/minercommand/v1/command_pb";

vi.mock("@/shared/assets/icons", () => ({
  Fleet: vi.fn(() => <div data-testid="fleet-icon" />),
}));

vi.mock("@/shared/components/Button", () => ({
  sizes: { base: "base" },
  variants: { primary: "primary", secondary: "secondary" },
}));

vi.mock("@/shared/components/ButtonGroup", () => ({
  groupVariants: { leftAligned: "leftAligned" },
}));

vi.mock("@/shared/components/Dialog", () => ({
  default: vi.fn(({ open, title, subtitle, buttons, testId }) =>
    open ? (
      <div data-testid={testId ?? "dialog"}>
        <div data-testid="dialog-title">{title}</div>
        <div data-testid="dialog-subtitle">{subtitle}</div>
        {buttons?.map((b: { text: string; onClick: () => void; testId?: string }, i: number) => (
          <button key={i} onClick={b.onClick} data-testid={b.testId ?? `dialog-button-${i}`}>
            {b.text}
          </button>
        ))}
      </div>
    ) : null,
  ),
}));

vi.mock("@/shared/components/Divider", () => ({
  default: vi.fn(() => <hr data-testid="divider" />),
}));

vi.mock("@/shared/components/Modal", () => ({
  default: vi.fn(({ open, buttons, children }) =>
    open ? (
      <div data-testid="modal">
        <div data-testid="modal-content">{children}</div>
        {buttons?.map((b: { text: string; onClick: () => void; testId?: string }, i: number) => (
          <button key={i} onClick={b.onClick} data-testid={b.testId ?? `modal-button-${i}`}>
            {b.text}
          </button>
        ))}
      </div>
    ) : null,
  ),
}));

vi.mock("@/shared/components/Row", () => ({
  default: vi.fn(({ children }) => <div data-testid="row">{children}</div>),
}));

const makeGroup = (
  overrides: Partial<{ firmwareVersion: string; model: string; count: number }>,
): UnsupportedMinerGroup =>
  ({
    firmwareVersion: "v20240702",
    model: "Antminer S21",
    count: 4,
    ...overrides,
  }) as unknown as UnsupportedMinerGroup;

describe("UnsupportedMinersModal", () => {
  const mockOnContinue = vi.fn();
  const mockOnDismiss = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe("noneSupported=true — Dialog", () => {
    it("renders Dialog when open is true", () => {
      render(
        <UnsupportedMinersModal
          open={true}
          unsupportedGroups={[makeGroup({})]}
          totalUnsupportedCount={4}
          noneSupported={true}
          onContinue={mockOnContinue}
          onDismiss={mockOnDismiss}
        />,
      );
      expect(screen.getByTestId("action-not-supported-dialog")).toBeInTheDocument();
      expect(screen.queryByTestId("modal")).not.toBeInTheDocument();
    });

    it("does not render when open is false", () => {
      render(
        <UnsupportedMinersModal
          open={false}
          unsupportedGroups={[makeGroup({})]}
          totalUnsupportedCount={4}
          noneSupported={true}
          onContinue={mockOnContinue}
          onDismiss={mockOnDismiss}
        />,
      );
      expect(screen.queryByTestId("action-not-supported-dialog")).not.toBeInTheDocument();
    });

    it("shows 'Action not supported' title", () => {
      render(
        <UnsupportedMinersModal
          open={true}
          unsupportedGroups={[makeGroup({})]}
          totalUnsupportedCount={4}
          noneSupported={true}
          onContinue={mockOnContinue}
          onDismiss={mockOnDismiss}
        />,
      );
      expect(screen.getByTestId("dialog-title")).toHaveTextContent("Action not supported");
    });

    it("uses plural 'miners'' in subtitle when count > 1", () => {
      render(
        <UnsupportedMinersModal
          open={true}
          unsupportedGroups={[makeGroup({})]}
          totalUnsupportedCount={4}
          noneSupported={true}
          onContinue={mockOnContinue}
          onDismiss={mockOnDismiss}
        />,
      );
      expect(screen.getByTestId("dialog-subtitle")).toHaveTextContent("miners'");
    });

    it("uses singular 'miner's' in subtitle when count is 1", () => {
      render(
        <UnsupportedMinersModal
          open={true}
          unsupportedGroups={[makeGroup({})]}
          totalUnsupportedCount={1}
          noneSupported={true}
          onContinue={mockOnContinue}
          onDismiss={mockOnDismiss}
        />,
      );
      expect(screen.getByTestId("dialog-subtitle")).toHaveTextContent("miner's");
    });

    it("shows Dismiss button and calls onDismiss when clicked", () => {
      render(
        <UnsupportedMinersModal
          open={true}
          unsupportedGroups={[makeGroup({})]}
          totalUnsupportedCount={4}
          noneSupported={true}
          onContinue={mockOnContinue}
          onDismiss={mockOnDismiss}
        />,
      );
      fireEvent.click(screen.getByTestId("dismiss-button"));
      expect(mockOnDismiss).toHaveBeenCalledOnce();
      expect(mockOnContinue).not.toHaveBeenCalled();
    });
  });

  describe("noneSupported=false — Modal with rows", () => {
    it("renders Modal when open is true", () => {
      render(
        <UnsupportedMinersModal
          open={true}
          unsupportedGroups={[makeGroup({})]}
          totalUnsupportedCount={4}
          noneSupported={false}
          onContinue={mockOnContinue}
          onDismiss={mockOnDismiss}
        />,
      );
      expect(screen.getByTestId("modal")).toBeInTheDocument();
      expect(screen.queryByTestId("action-not-supported-dialog")).not.toBeInTheDocument();
    });

    it("does not render when open is false", () => {
      render(
        <UnsupportedMinersModal
          open={false}
          unsupportedGroups={[makeGroup({})]}
          totalUnsupportedCount={4}
          noneSupported={false}
          onContinue={mockOnContinue}
          onDismiss={mockOnDismiss}
        />,
      );
      expect(screen.queryByTestId("modal")).not.toBeInTheDocument();
    });

    it("shows correct title and description in body", () => {
      render(
        <UnsupportedMinersModal
          open={true}
          unsupportedGroups={[makeGroup({})]}
          totalUnsupportedCount={12}
          noneSupported={false}
          onContinue={mockOnContinue}
          onDismiss={mockOnDismiss}
        />,
      );
      expect(screen.getByText("Some miners do not support this action.")).toBeInTheDocument();
      expect(screen.getByText("This action will be skipped for 12 miners.")).toBeInTheDocument();
    });

    it("shows Continue button and calls onContinue when clicked", () => {
      render(
        <UnsupportedMinersModal
          open={true}
          unsupportedGroups={[makeGroup({})]}
          totalUnsupportedCount={4}
          noneSupported={false}
          onContinue={mockOnContinue}
          onDismiss={mockOnDismiss}
        />,
      );
      fireEvent.click(screen.getByTestId("continue-button"));
      expect(mockOnContinue).toHaveBeenCalledOnce();
      expect(mockOnDismiss).not.toHaveBeenCalled();
    });

    it("renders a row for each unsupported group", () => {
      const groups = [
        makeGroup({ firmwareVersion: "v20240702", model: "Antminer S21" }),
        makeGroup({ firmwareVersion: "v20240703", model: "Antminer S19 XP" }),
        makeGroup({ firmwareVersion: "v20240704", model: "Antminer S19 Pro" }),
      ];
      render(
        <UnsupportedMinersModal
          open={true}
          unsupportedGroups={groups}
          totalUnsupportedCount={12}
          noneSupported={false}
          onContinue={mockOnContinue}
          onDismiss={mockOnDismiss}
        />,
      );
      expect(screen.getAllByTestId("row")).toHaveLength(3);
    });

    it("displays firmware version and model for each group", () => {
      render(
        <UnsupportedMinersModal
          open={true}
          unsupportedGroups={[makeGroup({ firmwareVersion: "v20240702", model: "Antminer S21" })]}
          totalUnsupportedCount={4}
          noneSupported={false}
          onContinue={mockOnContinue}
          onDismiss={mockOnDismiss}
        />,
      );
      expect(screen.getByText("Firmware v20240702")).toBeInTheDocument();
      expect(screen.getByText("Antminer S21")).toBeInTheDocument();
    });

    it("shows plural 'miners' for count greater than 1", () => {
      render(
        <UnsupportedMinersModal
          open={true}
          unsupportedGroups={[makeGroup({ count: 5 })]}
          totalUnsupportedCount={5}
          noneSupported={false}
          onContinue={mockOnContinue}
          onDismiss={mockOnDismiss}
        />,
      );
      expect(screen.getByText("5 miners")).toBeInTheDocument();
    });

    it("shows singular 'miner' for count of 1", () => {
      render(
        <UnsupportedMinersModal
          open={true}
          unsupportedGroups={[makeGroup({ count: 1 })]}
          totalUnsupportedCount={1}
          noneSupported={false}
          onContinue={mockOnContinue}
          onDismiss={mockOnDismiss}
        />,
      );
      expect(screen.getByText("1 miner")).toBeInTheDocument();
    });

    it("renders dividers between groups but not after the last one", () => {
      const groups = [
        makeGroup({ firmwareVersion: "v20240702", model: "Antminer S21" }),
        makeGroup({ firmwareVersion: "v20240703", model: "Antminer S19 XP" }),
        makeGroup({ firmwareVersion: "v20240704", model: "Antminer S19 Pro" }),
      ];
      render(
        <UnsupportedMinersModal
          open={true}
          unsupportedGroups={groups}
          totalUnsupportedCount={12}
          noneSupported={false}
          onContinue={mockOnContinue}
          onDismiss={mockOnDismiss}
        />,
      );
      // 3 groups → 2 dividers (between groups, not after last)
      expect(screen.getAllByTestId("divider")).toHaveLength(2);
    });

    it("renders no dividers for a single group", () => {
      render(
        <UnsupportedMinersModal
          open={true}
          unsupportedGroups={[makeGroup({})]}
          totalUnsupportedCount={4}
          noneSupported={false}
          onContinue={mockOnContinue}
          onDismiss={mockOnDismiss}
        />,
      );
      expect(screen.queryByTestId("divider")).not.toBeInTheDocument();
    });
  });
});
