import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import { StatusModal } from "./StatusModal";
import type { ComponentStatusData, MinerStatusData } from "./types";
import { variants } from "@/shared/components/Button";

// Component address type for navigation (used in stories/tests)
// Implementers should provide their own address type via generics
type ComponentAddress = {
  source: string;
  componentIndex?: number;
  [key: string]: any; // Allow additional fields for flexibility
};

// Mock the Modal component
vi.mock("@/shared/components/Modal", () => ({
  default: vi.fn(({ children, title, open, buttons, icon, onIconClick, onDismiss }) => {
    if (open === false) return null;
    return (
      <div data-testid="modal">
        <div data-testid="modal-title">{title}</div>
        {icon ? (
          <button data-testid="modal-icon" onClick={onIconClick}>
            {icon}
          </button>
        ) : null}
        <button data-testid="modal-dismiss" onClick={onDismiss}>
          Dismiss
        </button>
        {buttons?.map((btn: any, idx: number) => (
          <button key={idx} data-testid={`modal-button-${idx}`} onClick={btn.onClick}>
            {btn.text}
          </button>
        ))}
        <div data-testid="modal-content">{children}</div>
      </div>
    );
  }),
}));

describe("StatusModal", () => {
  const mockCloseModal = vi.fn();
  const mockNavigateBack = vi.fn();

  const mockMinerStatusData: MinerStatusData = {
    props: {
      title: "Miner is operating normally",
      subtitle: "All systems running",
      errors: {
        hashboard: [],
        psu: [],
        fan: [],
        controlBoard: [],
        other: [],
      },
      isSleeping: false,
    },
    title: "Miner status",
    buttons: [
      {
        text: "Done",
        variant: variants.primary,
        onClick: mockCloseModal,
      },
    ],
    onDismiss: mockCloseModal,
  };

  const mockComponentStatusData: ComponentStatusData = {
    props: {
      summary: "Hashboard 1 status",
      componentType: "hashboard",
      errors: [],
      metrics: [
        { label: "Temperature", value: "85°C" },
        { label: "Hashrate", value: "100 TH/s" },
      ],
    },
    title: "Hashboard status",
    buttons: [
      {
        text: "Done",
        variant: variants.primary,
        onClick: mockCloseModal,
      },
    ],
    onDismiss: mockCloseModal,
    onNavigateBack: mockNavigateBack,
  };

  const getMinerStatus = vi.fn(() => mockMinerStatusData);
  const getComponentStatus = vi.fn(() => mockComponentStatusData);

  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe("Miner Status View", () => {
    it("should render miner status when componentAddress is undefined", () => {
      render(
        <StatusModal
          componentAddress={undefined}
          getMinerStatus={getMinerStatus}
          getComponentStatus={getComponentStatus}
        />,
      );

      expect(screen.getByTestId("modal")).toBeInTheDocument();
      expect(screen.getByTestId("modal-title")).toHaveTextContent("Miner status");
      expect(getMinerStatus).toHaveBeenCalledTimes(1);
      expect(getComponentStatus).not.toHaveBeenCalled();
    });

    it("should not show back button for miner status view", () => {
      render(
        <StatusModal
          componentAddress={undefined}
          getMinerStatus={getMinerStatus}
          getComponentStatus={getComponentStatus}
          showBackButton={true}
        />,
      );

      expect(screen.queryByTestId("modal-icon")).not.toBeInTheDocument();
    });

    it("should handle dismiss callback", async () => {
      const user = userEvent.setup();
      render(
        <StatusModal
          componentAddress={undefined}
          getMinerStatus={getMinerStatus}
          getComponentStatus={getComponentStatus}
        />,
      );

      await user.click(screen.getByTestId("modal-dismiss"));
      expect(mockCloseModal).toHaveBeenCalledTimes(1);
    });

    it("should render miner status content", () => {
      render(
        <StatusModal
          componentAddress={undefined}
          getMinerStatus={getMinerStatus}
          getComponentStatus={getComponentStatus}
        />,
      );

      const content = screen.getByTestId("modal-content");
      expect(content).toBeInTheDocument();
      expect(content).toHaveTextContent("Miner is operating normally");
    });
  });

  describe("Component Status View", () => {
    const componentAddress: ComponentAddress = {
      source: "HASHBOARD",
      componentIndex: 0,
    };

    it("should render component status when componentAddress is defined", () => {
      render(
        <StatusModal
          componentAddress={componentAddress}
          getMinerStatus={getMinerStatus}
          getComponentStatus={getComponentStatus}
        />,
      );

      expect(screen.getByTestId("modal")).toBeInTheDocument();
      expect(screen.getByTestId("modal-title")).toHaveTextContent("Hashboard status");
      expect(getComponentStatus).toHaveBeenCalledWith(componentAddress);
      expect(getMinerStatus).not.toHaveBeenCalled();
    });

    it("should show back button when showBackButton is true and onNavigateBack exists", () => {
      render(
        <StatusModal
          componentAddress={componentAddress}
          getMinerStatus={getMinerStatus}
          getComponentStatus={getComponentStatus}
          showBackButton={true}
        />,
      );

      expect(screen.getByTestId("modal-icon")).toBeInTheDocument();
    });

    it("should not show back button when showBackButton is false", () => {
      render(
        <StatusModal
          componentAddress={componentAddress}
          getMinerStatus={getMinerStatus}
          getComponentStatus={getComponentStatus}
          showBackButton={false}
        />,
      );

      expect(screen.queryByTestId("modal-icon")).not.toBeInTheDocument();
    });

    it("should handle navigate back callback", async () => {
      const user = userEvent.setup();
      render(
        <StatusModal
          componentAddress={componentAddress}
          getMinerStatus={getMinerStatus}
          getComponentStatus={getComponentStatus}
          showBackButton={true}
        />,
      );

      await user.click(screen.getByTestId("modal-icon"));
      expect(mockNavigateBack).toHaveBeenCalledTimes(1);
    });

    it("should render component status content with metrics", () => {
      render(
        <StatusModal
          componentAddress={componentAddress}
          getMinerStatus={getMinerStatus}
          getComponentStatus={getComponentStatus}
        />,
      );

      const content = screen.getByTestId("modal-content");
      expect(content).toHaveTextContent("Hashboard 1 status");
      expect(content).toHaveTextContent("Temperature");
      expect(content).toHaveTextContent("85°C");
      expect(content).toHaveTextContent("Hashrate");
      expect(content).toHaveTextContent("100 TH/s");
    });
  });

  describe("Component Not Found", () => {
    it("should fallback to miner status when getComponentStatus returns undefined", () => {
      const getComponentStatusNotFound = vi.fn(() => undefined);
      const componentAddress: ComponentAddress = {
        source: "UNKNOWN" as any,
        componentIndex: 999,
      };

      render(
        <StatusModal
          componentAddress={componentAddress}
          getMinerStatus={getMinerStatus}
          getComponentStatus={getComponentStatusNotFound}
        />,
      );

      expect(screen.getByTestId("modal-title")).toHaveTextContent("Miner status");
      expect(getComponentStatusNotFound).toHaveBeenCalledWith(componentAddress);
      expect(getMinerStatus).toHaveBeenCalledTimes(1);
    });
  });

  describe("Modal Visibility", () => {
    it("should not render when open is false", () => {
      render(
        <StatusModal
          componentAddress={undefined}
          getMinerStatus={getMinerStatus}
          getComponentStatus={getComponentStatus}
          open={false}
        />,
      );

      expect(screen.queryByTestId("modal")).not.toBeInTheDocument();
      // Functions should still be called even when modal is hidden
      expect(getMinerStatus).toHaveBeenCalledTimes(1);
    });

    it("should render when open is true", () => {
      render(
        <StatusModal
          componentAddress={undefined}
          getMinerStatus={getMinerStatus}
          getComponentStatus={getComponentStatus}
          open={true}
        />,
      );

      expect(screen.getByTestId("modal")).toBeInTheDocument();
    });
  });

  describe("Button Actions", () => {
    it("should render all buttons from miner status data", () => {
      const multiButtonMinerStatus: MinerStatusData = {
        ...mockMinerStatusData,
        buttons: [
          {
            text: "Wake miner",
            variant: variants.secondary,
            onClick: vi.fn(),
          },
          {
            text: "Done",
            variant: variants.primary,
            onClick: mockCloseModal,
          },
        ],
      };

      render(
        <StatusModal
          componentAddress={undefined}
          getMinerStatus={() => multiButtonMinerStatus}
          getComponentStatus={getComponentStatus}
        />,
      );

      expect(screen.getByTestId("modal-button-0")).toHaveTextContent("Wake miner");
      expect(screen.getByTestId("modal-button-1")).toHaveTextContent("Done");
    });

    it("should call button onClick handlers", async () => {
      const user = userEvent.setup();
      const mockWakeMiner = vi.fn();
      const sleepingMinerStatus: MinerStatusData = {
        ...mockMinerStatusData,
        props: {
          ...mockMinerStatusData.props,
          isSleeping: true,
        },
        buttons: [
          {
            text: "Wake miner",
            variant: variants.secondary,
            onClick: mockWakeMiner,
          },
          {
            text: "Done",
            variant: variants.primary,
            onClick: mockCloseModal,
          },
        ],
      };

      render(
        <StatusModal
          componentAddress={undefined}
          getMinerStatus={() => sleepingMinerStatus}
          getComponentStatus={getComponentStatus}
        />,
      );

      await user.click(screen.getByTestId("modal-button-0"));
      expect(mockWakeMiner).toHaveBeenCalledTimes(1);

      await user.click(screen.getByTestId("modal-button-1"));
      expect(mockCloseModal).toHaveBeenCalledTimes(1);
    });
  });
});
