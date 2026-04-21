import React from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import UpdateWorkerNameDialog from "./UpdateWorkerNameDialog";
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
        <div data-testid="update-worker-name-modal">
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
      <div data-testid="update-worker-name-no-changes-dialog">
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

describe("UpdateWorkerNameDialog", () => {
  const mockOnConfirm = vi.fn();
  const mockOnDismiss = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("calls onConfirm with a trimmed worker name when Save is clicked", () => {
    render(
      <UpdateWorkerNameDialog
        open={true}
        currentWorkerName="worker-old"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
      />,
    );

    fireEvent.change(screen.getByTestId("update-worker-name-input"), { target: { value: "  worker-new  " } });
    fireEvent.click(screen.getByTestId("modal-button-0"));

    expect(mockOnConfirm).toHaveBeenCalledWith("worker-new");
  });

  it("submits the current worker name when no-change confirmation is accepted", () => {
    render(
      <UpdateWorkerNameDialog
        open={true}
        currentWorkerName="worker-old"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
      />,
    );

    fireEvent.click(screen.getByTestId("modal-button-0"));

    expect(screen.getByTestId("update-worker-name-no-changes-dialog")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("dialog-button-1"));

    expect(mockOnConfirm).toHaveBeenCalledWith("worker-old");
    expect(mockOnDismiss).not.toHaveBeenCalled();
  });

  it("keeps editing when no-change confirmation is accepted for an empty worker name", () => {
    render(
      <UpdateWorkerNameDialog open={true} currentWorkerName="" onConfirm={mockOnConfirm} onDismiss={mockOnDismiss} />,
    );

    fireEvent.click(screen.getByTestId("modal-button-0"));

    expect(screen.getByTestId("update-worker-name-no-changes-dialog")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("dialog-button-1"));

    expect(mockOnConfirm).not.toHaveBeenCalled();
    expect(screen.getByTestId("update-worker-name-modal")).toBeInTheDocument();
    expect(screen.queryByTestId("update-worker-name-no-changes-dialog")).not.toBeInTheDocument();
  });

  it("passes maxLength of 100 to the input", () => {
    render(
      <UpdateWorkerNameDialog
        open={true}
        currentWorkerName="worker-old"
        onConfirm={mockOnConfirm}
        onDismiss={mockOnDismiss}
      />,
    );

    const [firstCallProps] = vi.mocked(Input).mock.calls[0];
    expect(firstCallProps.maxLength).toBe(100);
  });
});
