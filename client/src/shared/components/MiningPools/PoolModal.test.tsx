import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import PoolModal from "./PoolModal";
import { PoolIndex } from "./types";

describe("PoolModal", () => {
  const mockOnChangePools = vi.fn();
  const mockOnDismiss = vi.fn();
  const mockTestConnection = vi.fn();
  const mockOnSave = vi.fn();

  const defaultProps = {
    onChangePools: mockOnChangePools,
    onDismiss: mockOnDismiss,
    poolIndex: 0 as PoolIndex,
    pools: [{ name: "", url: "", username: "", password: "", priority: 0 }],
    open: true,
    isTestingConnection: false,
    testConnection: mockTestConnection,
  };

  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe("username separator validation", () => {
    it("blocks saving when periods are disallowed in usernames", () => {
      render(
        <PoolModal
          {...defaultProps}
          pools={[
            {
              name: "Pool A",
              url: "stratum+tcp://pool.example.com:3333",
              username: "wallet.worker01",
              password: "",
              priority: 0,
            },
          ]}
          onSave={mockOnSave}
          disallowUsernameSeparator
        />,
      );

      fireEvent.click(screen.getByTestId("pool-save-button"));

      expect(mockOnSave).not.toHaveBeenCalled();
      expect(
        screen.getByText(
          "Fleet-level pool usernames can’t include periods (.). Set worker names on each miner instead.",
        ),
      ).toBeInTheDocument();
    });

    it("blocks connection tests when periods are disallowed in usernames", () => {
      render(
        <PoolModal
          {...defaultProps}
          pools={[
            {
              name: "Pool A",
              url: "stratum+tcp://pool.example.com:3333",
              username: "wallet.worker01",
              password: "",
              priority: 0,
            },
          ]}
          disallowUsernameSeparator
        />,
      );

      fireEvent.click(screen.getByText("Test connection"));

      expect(mockTestConnection).not.toHaveBeenCalled();
      expect(
        screen.getByText(
          "Fleet-level pool usernames can’t include periods (.). Set worker names on each miner instead.",
        ),
      ).toBeInTheDocument();
    });

    it("allows saving an unchanged legacy dotted username in edit mode", async () => {
      mockOnSave.mockResolvedValue(undefined);

      render(
        <PoolModal
          {...defaultProps}
          pools={[
            {
              name: "Pool A",
              url: "stratum+tcp://pool.example.com:3333",
              username: "wallet.worker01",
              password: "",
              priority: 0,
            },
          ]}
          onSave={mockOnSave}
          mode="edit"
          disallowUsernameSeparator
        />,
      );

      fireEvent.click(screen.getByTestId("pool-save-button"));

      await waitFor(() => {
        expect(mockOnSave).toHaveBeenCalledWith(expect.objectContaining({ username: "wallet.worker01" }), false);
      });
    });

    it("still blocks a newly entered dotted username in edit mode", () => {
      render(
        <PoolModal
          {...defaultProps}
          pools={[
            {
              name: "Pool A",
              url: "stratum+tcp://pool.example.com:3333",
              username: "wallet",
              password: "",
              priority: 0,
            },
          ]}
          onSave={mockOnSave}
          mode="edit"
          disallowUsernameSeparator
        />,
      );

      fireEvent.change(screen.getByLabelText("Username"), { target: { value: "wallet.worker01" } });
      fireEvent.click(screen.getByTestId("pool-save-button"));

      expect(mockOnSave).not.toHaveBeenCalled();
      expect(
        screen.getByText(
          "Fleet-level pool usernames can’t include periods (.). Set worker names on each miner instead.",
        ),
      ).toBeInTheDocument();
    });
  });

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

    it("renders a custom username label and helper text when provided", () => {
      const { getByLabelText } = render(
        <PoolModal
          {...defaultProps}
          usernameLabel="Username (optional)"
          usernameHelperText={
            <>
              To add a worker name, add a period after the username followed by the worker name.
              <br />
              Example: mann23.workerbee
            </>
          }
        />,
      );

      const usernameInput = getByLabelText("Username (optional)");
      const helperText = usernameInput.closest(".space-y-2")?.querySelector(".text-200.text-text-primary-70");

      expect(usernameInput).toBeInTheDocument();
      expect(helperText).toHaveTextContent(
        "To add a worker name, add a period after the username followed by the worker name.Example: mann23.workerbee",
      );
      expect(screen.getByText("Password (optional)")).toBeInTheDocument();
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

    it("allows saving without a username when usernameRequired is false", () => {
      render(
        <PoolModal
          {...defaultProps}
          usernameLabel="Username (optional)"
          usernameRequired={false}
          pools={[
            { name: "Pool A", url: "stratum+tcp://pool.example.com:3333", username: "", password: "", priority: 0 },
          ]}
        />,
      );

      expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
    });

    it("still requires a username by default", () => {
      render(
        <PoolModal
          {...defaultProps}
          pools={[
            { name: "Pool A", url: "stratum+tcp://pool.example.com:3333", username: "", password: "", priority: 0 },
          ]}
        />,
      );

      expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    });
  });
});
