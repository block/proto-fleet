import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import PoolModal from "./PoolModal";
import { PoolIndex } from "./types";

describe("PoolModal", () => {
  const mockOnChangePools = vi.fn();
  const mockOnDismiss = vi.fn();
  const mockTestConnection = vi.fn();

  const defaultProps = {
    onChangePools: mockOnChangePools,
    onDismiss: mockOnDismiss,
    poolIndex: 0 as PoolIndex,
    pools: [{ name: "", url: "", username: "", password: "", priority: 0 }],
    show: true,
    isTestingConnection: false,
    testConnection: mockTestConnection,
  };

  describe("autofocus behavior", () => {
    it("autofocuses the Pool Name input when hidePoolName is false", () => {
      const { getByLabelText } = render(<PoolModal {...defaultProps} hidePoolName={false} />);

      const poolNameInput = getByLabelText("Pool Name");
      expect(poolNameInput).toHaveFocus();
    });

    it("does not autofocus the Pool URL input when hidePoolName is false", () => {
      const { getByLabelText } = render(<PoolModal {...defaultProps} hidePoolName={false} />);

      const poolUrlInput = getByLabelText("Pool URL");
      expect(poolUrlInput).not.toHaveFocus();
    });

    it("autofocuses the Pool URL input when hidePoolName is true", () => {
      const { getByLabelText } = render(<PoolModal {...defaultProps} hidePoolName={true} />);

      const poolUrlInput = getByLabelText("Pool URL");
      expect(poolUrlInput).toHaveFocus();
    });

    it("does not render Pool Name input when hidePoolName is true", () => {
      const { queryByLabelText } = render(<PoolModal {...defaultProps} hidePoolName={true} />);

      const poolNameInput = queryByLabelText("Pool Name");
      expect(poolNameInput).not.toBeInTheDocument();
    });
  });

  describe("basic rendering", () => {
    it("renders all input fields when hidePoolName is false", () => {
      const { getByLabelText } = render(<PoolModal {...defaultProps} hidePoolName={false} />);

      expect(getByLabelText("Pool Name")).toBeInTheDocument();
      expect(getByLabelText("Pool URL")).toBeInTheDocument();
      expect(getByLabelText("Username")).toBeInTheDocument();
      expect(getByLabelText("Password (optional)")).toBeInTheDocument();
    });

    it("renders Save and Test connection buttons", () => {
      const { getByText } = render(<PoolModal {...defaultProps} />);

      expect(getByText("Save")).toBeInTheDocument();
      expect(getByText("Test connection")).toBeInTheDocument();
    });

    it("allows input values to be changed", () => {
      const { getByLabelText } = render(<PoolModal {...defaultProps} />);

      const urlInput = getByLabelText("Pool URL");
      fireEvent.change(urlInput, { target: { value: "stratum+tcp://pool.example.com:3333" } });

      expect(urlInput).toHaveValue("stratum+tcp://pool.example.com:3333");
    });
  });
});
