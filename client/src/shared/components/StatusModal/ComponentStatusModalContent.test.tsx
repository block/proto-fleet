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
  describe("icon container styling", () => {
    it("should wrap the icon in a container with rounded-lg class", () => {
      render(<ComponentStatusModalContent {...baseProps} />);

      const icon = screen.getByTestId("hashboard-icon");
      const container = icon.parentElement!;
      expect(container.tagName).toBe("DIV");
      expect(container.className).toContain("rounded-lg");
    });

    it("should have p-2 padding on the icon container", () => {
      render(<ComponentStatusModalContent {...baseProps} />);

      const icon = screen.getByTestId("hashboard-icon");
      const container = icon.parentElement!;
      expect(container.className).toContain("p-2");
    });

    it("should use bg-core-primary-5 container when there are no errors", () => {
      render(<ComponentStatusModalContent {...baseProps} />);

      const icon = screen.getByTestId("hashboard-icon");
      const container = icon.parentElement!;
      expect(container.className).toContain("bg-core-primary-5");
    });

    it("should use text-core-primary-20 on icon when there are no errors", () => {
      render(<ComponentStatusModalContent {...baseProps} />);

      const icon = screen.getByTestId("hashboard-icon");
      expect(icon.className).toContain("text-core-primary-20");
    });

    it("should use bg-intent-critical-10 container when there are errors", () => {
      const propsWithErrors: ComponentStatusModalProps = {
        ...baseProps,
        errors: [
          {
            componentName: "Hashboard 1",
            message: "Hashboard is not responding",
            timestamp: 1700000000,
          },
        ],
      };

      render(<ComponentStatusModalContent {...propsWithErrors} />);

      const icon = screen.getByTestId("hashboard-icon");
      const container = icon.parentElement!;
      expect(container.className).toContain("bg-intent-critical-10");
    });

    it("should use text-intent-critical-fill on icon when there are errors", () => {
      const propsWithErrors: ComponentStatusModalProps = {
        ...baseProps,
        errors: [
          {
            componentName: "Hashboard 1",
            message: "Hashboard is not responding",
            timestamp: 1700000000,
          },
        ],
      };

      render(<ComponentStatusModalContent {...propsWithErrors} />);

      const icon = screen.getByTestId("hashboard-icon");
      expect(icon.className).toContain("text-intent-critical-fill");
    });

    it("should not use primary styling when there are errors", () => {
      const propsWithErrors: ComponentStatusModalProps = {
        ...baseProps,
        errors: [
          {
            componentName: "Hashboard 1",
            message: "Hashboard is not responding",
            timestamp: 1700000000,
          },
        ],
      };

      render(<ComponentStatusModalContent {...propsWithErrors} />);

      const icon = screen.getByTestId("hashboard-icon");
      const container = icon.parentElement!;
      expect(container.className).not.toContain("bg-core-primary-5");
      expect(icon.className).not.toContain("text-core-primary-20");
    });

    it("should not use critical styling when there are no errors", () => {
      render(<ComponentStatusModalContent {...baseProps} />);

      const icon = screen.getByTestId("hashboard-icon");
      const container = icon.parentElement!;
      expect(container.className).not.toContain("bg-intent-critical-10");
      expect(icon.className).not.toContain("text-intent-critical-fill");
    });
  });

  describe("icon container per component type", () => {
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
      const icon = screen.getByTestId("alert-icon");
      expect(icon).toBeInTheDocument();
      const container = icon.parentElement!;
      expect(container.className).toContain("rounded-lg");
    });
  });
});
