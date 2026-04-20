import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import ComponentStatusModalContent from "./ComponentStatusModalContent";
import type { ComponentStatusModalProps } from "./types";

vi.mock("@/shared/assets/icons", () => ({
  Alert: vi.fn((props) => <span data-testid="alert-icon" className={props.className} />),
  ControlBoard: vi.fn((props) => <span data-testid="controlboard-icon" className={props.className} />),
  Fan: vi.fn((props) => <span data-testid="fan-icon" className={props.className} />),
  Hashboard: vi.fn((props) => <span data-testid="hashboard-icon" className={props.className} />),
  LightningAlt: vi.fn((props) => <span data-testid="psu-icon" className={props.className} />),
}));

vi.mock("@/shared/assets/icons/constants", () => ({
  iconSizes: {
    xSmall: "w-3",
    small: "w-4",
    medium: "w-5",
    xLarge: "w-8",
  },
}));

const baseProps: ComponentStatusModalProps = {
  summary: "Hashboard 1 status",
  componentType: "hashboard",
  errors: [],
  metrics: [
    { label: "Temperature", value: "85°C" },
    { label: "Hashrate", value: "100 TH/s" },
  ],
};

describe("ComponentStatusModalContent", () => {
  describe("icon per component type", () => {
    it("should render Hashboard icon for hashboard component type", () => {
      render(<ComponentStatusModalContent {...baseProps} />);
      expect(screen.getByTestId("hashboard-icon")).toBeInTheDocument();
    });

    it("should render Fan icon for fan component type", () => {
      render(<ComponentStatusModalContent {...baseProps} componentType="fan" />);
      expect(screen.getByTestId("fan-icon")).toBeInTheDocument();
    });

    it("should render LightningAlt icon for psu component type", () => {
      render(<ComponentStatusModalContent {...baseProps} componentType="psu" />);
      expect(screen.getByTestId("psu-icon")).toBeInTheDocument();
    });

    it("should render ControlBoard icon for controlBoard component type", () => {
      render(<ComponentStatusModalContent {...baseProps} componentType="controlBoard" />);
      expect(screen.getByTestId("controlboard-icon")).toBeInTheDocument();
    });

    it("should render Alert icon for other component type", () => {
      render(<ComponentStatusModalContent {...baseProps} componentType="other" />);
      expect(screen.getByTestId("alert-icon")).toBeInTheDocument();
    });
  });
});
