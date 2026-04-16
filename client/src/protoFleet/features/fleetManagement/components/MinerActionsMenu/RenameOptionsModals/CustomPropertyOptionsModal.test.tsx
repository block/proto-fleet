import React from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import CustomPropertyOptionsModal from "./CustomPropertyOptionsModal";
import { customPropertyTypes } from "./types";

vi.mock("@/shared/components/Modal/Modal", () => ({
  default: vi.fn(
    ({
      open,
      children,
      buttons,
      title,
    }: {
      open: boolean;
      children: React.ReactNode;
      buttons?: { text: string; onClick: () => void; disabled?: boolean; testId?: string }[];
      title: string;
    }) => {
      if (!open) return null;

      return (
        <div data-testid="custom-property-options-modal">
          <h2>{title}</h2>
          {children}
          {buttons?.map((button, index) => (
            <button
              key={button.testId ?? index}
              onClick={button.onClick}
              disabled={button.disabled}
              data-testid={button.testId ?? `modal-button-${index}`}
            >
              {button.text}
            </button>
          ))}
        </div>
      );
    },
  ),
}));

vi.mock("@/shared/components/NamePreview", () => ({
  PreviewContainer: vi.fn(({ children }: { children: React.ReactNode }) => (
    <div data-testid="preview-container">{children}</div>
  )),
}));

describe("CustomPropertyOptionsModal", () => {
  const onConfirm = vi.fn();
  const onDismiss = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("shows prefix, suffix, and counter fields by default", () => {
    render(<CustomPropertyOptionsModal open={true} previewName="M-001" onConfirm={onConfirm} onDismiss={onDismiss} />);

    expect(screen.getByTestId("custom-property-prefix-input")).toBeInTheDocument();
    expect(screen.getByTestId("custom-property-suffix-input")).toBeInTheDocument();
    expect(screen.getByTestId("custom-property-counter-start-input")).toBeInTheDocument();
    expect(screen.getByTestId("custom-property-counter-scale-option-1")).toBeInTheDocument();
    expect(screen.queryByTestId("custom-property-string-input")).not.toBeInTheDocument();
    expect(screen.getByText("Options")).toBeInTheDocument();

    expect(screen.getByTestId("custom-property-counter-start-input")).toHaveValue("");
    expect(screen.getByTestId("custom-property-options-save-button")).toBeDisabled();
    expect(screen.getByText("Enter prefix, suffix, or counter to preview")).toBeInTheDocument();
    expect(screen.queryByTestId("name-preview")).not.toBeInTheDocument();
  });

  it("applies 100 character max length to custom option inputs", () => {
    render(<CustomPropertyOptionsModal open={true} previewName="M-001" onConfirm={onConfirm} onDismiss={onDismiss} />);

    expect(screen.getByTestId("custom-property-prefix-input")).toHaveAttribute("maxLength", "100");
    expect(screen.getByTestId("custom-property-suffix-input")).toHaveAttribute("maxLength", "100");
    expect(screen.getByTestId("custom-property-counter-start-input")).toHaveAttribute("maxLength", "9");

    fireEvent.click(screen.getByTestId("custom-property-type-button"));
    fireEvent.click(screen.getByTestId(`custom-property-type-option-${customPropertyTypes.stringOnly}`));

    expect(screen.getByTestId("custom-property-string-input")).toHaveAttribute("maxLength", "100");
  });

  it("changes fields based on selected type", () => {
    render(<CustomPropertyOptionsModal open={true} previewName="M-001" onConfirm={onConfirm} onDismiss={onDismiss} />);

    fireEvent.click(screen.getByTestId("custom-property-type-button"));
    fireEvent.click(screen.getByTestId(`custom-property-type-option-${customPropertyTypes.counterOnly}`));

    expect(screen.queryByTestId("custom-property-prefix-input")).not.toBeInTheDocument();
    expect(screen.queryByTestId("custom-property-suffix-input")).not.toBeInTheDocument();
    expect(screen.getByTestId("custom-property-counter-start-input")).toBeInTheDocument();
    expect(screen.getByTestId("custom-property-counter-scale-option-1")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("custom-property-type-button"));
    fireEvent.click(screen.getByTestId(`custom-property-type-option-${customPropertyTypes.stringOnly}`));

    expect(screen.getByTestId("custom-property-string-input")).toBeInTheDocument();
    expect(screen.queryByTestId("custom-property-counter-start-input")).not.toBeInTheDocument();
    expect(screen.queryByTestId("custom-property-counter-scale-option-3")).not.toBeInTheDocument();
  });

  it("submits selected counter scale", () => {
    render(<CustomPropertyOptionsModal open={true} previewName="M-001" onConfirm={onConfirm} onDismiss={onDismiss} />);

    fireEvent.change(screen.getByTestId("custom-property-counter-start-input"), { target: { value: "7" } });
    fireEvent.click(screen.getByTestId("custom-property-counter-scale-option-6"));
    fireEvent.click(screen.getByTestId("custom-property-options-save-button"));

    expect(onConfirm).toHaveBeenCalledWith(
      expect.objectContaining({
        counterStart: 7,
        counterScale: 6,
      }),
    );
  });

  it("shows counter placeholder only for counter-only type when counter is empty", () => {
    render(
      <CustomPropertyOptionsModal
        open={true}
        previewName="M-001"
        initialValues={{ type: customPropertyTypes.counterOnly }}
        onConfirm={onConfirm}
        onDismiss={onDismiss}
      />,
    );

    expect(screen.getByText("Enter counter to preview")).toBeInTheDocument();
  });

  it("shows prefix and suffix preview when counter is empty in custom string + counter", () => {
    render(<CustomPropertyOptionsModal open={true} previewName="M-001" onConfirm={onConfirm} onDismiss={onDismiss} />);

    fireEvent.change(screen.getByTestId("custom-property-prefix-input"), { target: { value: "Rack-" } });
    fireEvent.change(screen.getByTestId("custom-property-suffix-input"), { target: { value: "-A" } });

    expect(screen.queryByText("Enter prefix, suffix, or counter to preview")).not.toBeInTheDocument();
    expect(screen.getByTestId("custom-property-preview-leading")).toHaveTextContent("");
    expect(screen.getByTestId("custom-property-preview-highlighted")).toHaveTextContent("Rack--A");
    expect(screen.getByTestId("custom-property-preview-trailing")).toHaveTextContent("");
  });

  it("requires string input for string-only type", () => {
    render(
      <CustomPropertyOptionsModal
        open={true}
        previewName="M-001"
        initialValues={{ type: customPropertyTypes.stringOnly, stringValue: "" }}
        onConfirm={onConfirm}
        onDismiss={onDismiss}
      />,
    );

    const saveButton = screen.getByTestId("custom-property-options-save-button");
    expect(saveButton).toBeDisabled();

    fireEvent.change(screen.getByTestId("custom-property-string-input"), { target: { value: "  Rack A  " } });

    expect(saveButton).toBeEnabled();

    fireEvent.click(saveButton);
    expect(onConfirm).toHaveBeenCalledWith(
      expect.objectContaining({
        type: customPropertyTypes.stringOnly,
        stringValue: "Rack A",
      }),
    );
  });

  it("renders preview in new-name-only mode", () => {
    render(
      <CustomPropertyOptionsModal
        open={true}
        previewName="M-001"
        initialValues={{ counterStart: 1 }}
        onConfirm={onConfirm}
        onDismiss={onDismiss}
      />,
    );

    expect(screen.getByTestId("custom-property-preview-leading")).toHaveTextContent("");
    expect(screen.getByTestId("custom-property-preview-highlighted")).toHaveTextContent("M-001");
    expect(screen.getByTestId("custom-property-preview-trailing")).toHaveTextContent("");
  });
});
