import React from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import RenameMinerDialog from "./RenameMinerDialog";
import Input from "@/shared/components/Input";

vi.mock("@/shared/components/Modal/Modal", () => ({
  default: vi.fn(
    ({
      open,
      children,
      buttons,
      onDismiss,
      title,
    }: {
      open: boolean;
      children: React.ReactNode;
      buttons?: { text: string; onClick: () => void; variant?: string; dismissModalOnClick?: boolean }[];
      onDismiss: () => void;
      title: string;
    }) => {
      if (!open) return null;
      return (
        <div data-testid="rename-modal">
          <h2>{title}</h2>
          {children}
          <button onClick={onDismiss} data-testid="modal-dismiss">
            Dismiss
          </button>
          {buttons?.map((button, index) => (
            <button
              key={index}
              onClick={() => {
                button.onClick();
                if (button.dismissModalOnClick !== false) onDismiss();
              }}
              data-testid={`modal-button-${index}`}
            >
              {button.text}
            </button>
          ))}
        </div>
      );
    },
  ),
}));

vi.mock("@/shared/components/Dialog", () => ({
  default: vi.fn(({ open, title, buttons }) => {
    if (!open) return null;
    return (
      <div data-testid="rename-miner-no-changes-dialog">
        <h2>{title}</h2>
        {buttons?.map((button: { text: string; onClick: () => void }, index: number) => (
          <button key={index} onClick={button.onClick} data-testid={`dialog-button-${index}`}>
            {button.text}
          </button>
        ))}
      </div>
    );
  }),
}));

vi.mock("@/shared/components/Input", () => ({
  default: vi.fn(({ id, label, initValue, onChange, onKeyDown, testId }) => (
    <div>
      <label htmlFor={id}>{label}</label>
      <input
        id={id}
        defaultValue={initValue}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => onKeyDown?.(e.key)}
        data-testid={testId ?? id}
      />
    </div>
  )),
}));

vi.mock("@/shared/components/NamePreview", () => ({
  default: vi.fn(({ currentName, newName }: { currentName: string; newName: string }) => (
    <div data-testid="name-preview" data-current-name={currentName} data-new-name={newName} />
  )),
}));

describe("RenameMinerDialog", () => {
  const mockOnConfirm = vi.fn();
  const mockOnDismiss = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe("Preview", () => {
    it("passes current name as both props on open", () => {
      render(
        <RenameMinerDialog
          open={true}
          deviceIdentifier="device-1"
          currentMinerName="My Miner"
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const preview = screen.getByTestId("name-preview");
      expect(preview).toHaveAttribute("data-current-name", "My Miner");
      expect(preview).toHaveAttribute("data-new-name", "My Miner");
    });

    it("passes input value as newName after typing", () => {
      render(
        <RenameMinerDialog
          open={true}
          deviceIdentifier="device-1"
          currentMinerName="My Miner"
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      fireEvent.change(screen.getByTestId("rename-miner-input"), { target: { value: "New Name" } });

      const preview = screen.getByTestId("name-preview");
      expect(preview).toHaveAttribute("data-current-name", "My Miner");
      expect(preview).toHaveAttribute("data-new-name", "New Name");
    });
  });

  describe("Save", () => {
    it("calls onConfirm with trimmed name when Save is clicked", () => {
      render(
        <RenameMinerDialog
          open={true}
          deviceIdentifier="device-1"
          currentMinerName="My Miner"
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      fireEvent.change(screen.getByTestId("rename-miner-input"), { target: { value: "  Trimmed Name  " } });
      fireEvent.click(screen.getByTestId("modal-button-0"));

      expect(mockOnConfirm).toHaveBeenCalledWith("Trimmed Name");
    });

    it("calls onConfirm when Enter key is pressed in the input", () => {
      render(
        <RenameMinerDialog
          open={true}
          deviceIdentifier="device-1"
          currentMinerName="My Miner"
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      fireEvent.change(screen.getByTestId("rename-miner-input"), { target: { value: "New Name" } });
      fireEvent.keyDown(screen.getByTestId("rename-miner-input"), { key: "Enter" });

      expect(mockOnConfirm).toHaveBeenCalledWith("New Name");
    });
  });

  describe("No-changes warning", () => {
    it("shows warning when saving a name equal to current name with surrounding whitespace", () => {
      render(
        <RenameMinerDialog
          open={true}
          deviceIdentifier="device-1"
          currentMinerName="  Padded Name  "
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      // Typing the trimmed equivalent should be treated as no change
      fireEvent.change(screen.getByTestId("rename-miner-input"), { target: { value: "Padded Name" } });
      fireEvent.click(screen.getByTestId("modal-button-0"));

      expect(screen.getByTestId("rename-miner-no-changes-dialog")).toBeInTheDocument();
      expect(mockOnConfirm).not.toHaveBeenCalled();
    });

    it("shows warning dialog when saving without changing the name", () => {
      render(
        <RenameMinerDialog
          open={true}
          deviceIdentifier="device-1"
          currentMinerName="My Miner"
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      // Click Save without changing the input — name equals currentName
      fireEvent.click(screen.getByTestId("modal-button-0"));

      expect(mockOnConfirm).not.toHaveBeenCalled();
      expect(screen.getByTestId("rename-miner-no-changes-dialog")).toBeInTheDocument();
    });

    it("shows warning dialog instead of calling onConfirm when name is empty", () => {
      render(
        <RenameMinerDialog
          open={true}
          deviceIdentifier="device-1"
          currentMinerName="My Miner"
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      fireEvent.change(screen.getByTestId("rename-miner-input"), { target: { value: "   " } });
      fireEvent.click(screen.getByTestId("modal-button-0"));

      expect(mockOnConfirm).not.toHaveBeenCalled();
      expect(screen.getByTestId("rename-miner-no-changes-dialog")).toBeInTheDocument();
    });

    it("returns to rename modal when 'No, keep editing' is clicked in warning", () => {
      render(
        <RenameMinerDialog
          open={true}
          deviceIdentifier="device-1"
          currentMinerName="My Miner"
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      fireEvent.change(screen.getByTestId("rename-miner-input"), { target: { value: "" } });
      fireEvent.click(screen.getByTestId("modal-button-0"));

      // "No, keep editing" is the first button (index 0)
      fireEvent.click(screen.getByTestId("dialog-button-0"));

      expect(screen.getByTestId("rename-modal")).toBeInTheDocument();
      expect(screen.queryByTestId("rename-miner-no-changes-dialog")).not.toBeInTheDocument();
    });

    it("calls onDismiss when 'Yes, continue' is clicked in warning", () => {
      render(
        <RenameMinerDialog
          open={true}
          deviceIdentifier="device-1"
          currentMinerName="My Miner"
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      fireEvent.change(screen.getByTestId("rename-miner-input"), { target: { value: "" } });
      fireEvent.click(screen.getByTestId("modal-button-0"));

      // "Yes, continue" is the second button (index 1)
      fireEvent.click(screen.getByTestId("dialog-button-1"));

      expect(mockOnDismiss).toHaveBeenCalled();
    });
  });

  describe("Input constraints", () => {
    it("passes maxLength of 100 to the Input", () => {
      render(
        <RenameMinerDialog
          open={true}
          deviceIdentifier="device-1"
          currentMinerName="My Miner"
          onConfirm={mockOnConfirm}
          onDismiss={mockOnDismiss}
        />,
      );

      const [firstCallProps] = vi.mocked(Input).mock.calls[0];
      expect(firstCallProps.maxLength).toBe(100);
    });
  });
});
