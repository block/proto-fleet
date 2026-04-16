import React from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import QualifierOptionsModal from "./QualifierOptionsModal";

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
        <div data-testid="qualifier-options-modal">
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

describe("QualifierOptionsModal", () => {
  const onConfirm = vi.fn();
  const onDismiss = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders prefix and suffix fields", () => {
    render(<QualifierOptionsModal open={true} previewName="M-001" onConfirm={onConfirm} onDismiss={onDismiss} />);

    expect(screen.getByTestId("qualifier-property-prefix-input")).toBeInTheDocument();
    expect(screen.getByTestId("qualifier-property-suffix-input")).toBeInTheDocument();
    expect(screen.getByTestId("qualifier-property-prefix-input")).toHaveAttribute("maxLength", "100");
    expect(screen.getByTestId("qualifier-property-suffix-input")).toHaveAttribute("maxLength", "100");
  });

  it("submits trimmed prefix and suffix", () => {
    render(<QualifierOptionsModal open={true} previewName="M-001" onConfirm={onConfirm} onDismiss={onDismiss} />);

    fireEvent.change(screen.getByTestId("qualifier-property-prefix-input"), { target: { value: "  B1  " } });
    fireEvent.change(screen.getByTestId("qualifier-property-suffix-input"), { target: { value: "  -R4  " } });

    fireEvent.click(screen.getByTestId("qualifier-options-save-button"));

    expect(onConfirm).toHaveBeenCalledWith({
      prefix: "B1",
      suffix: "-R4",
    });
  });

  it("highlights entire preview name when no highlightedText is provided", () => {
    render(
      <QualifierOptionsModal
        open={true}
        previewName="TEXA-BA-R01-001-4D5E"
        onConfirm={onConfirm}
        onDismiss={onDismiss}
      />,
    );

    expect(screen.getByTestId("qualifier-preview-leading")).toHaveTextContent("");
    expect(screen.getByTestId("qualifier-preview-highlighted")).toHaveTextContent("TEXA-BA-R01-001-4D5E");
    expect(screen.getByTestId("qualifier-preview-trailing")).toHaveTextContent("");
  });

  it("highlights the specified text in preview", () => {
    render(
      <QualifierOptionsModal
        open={true}
        previewName="TEXA-BA-R01-001-4D5E"
        highlightedText="BA"
        onConfirm={onConfirm}
        onDismiss={onDismiss}
      />,
    );

    expect(screen.getByTestId("qualifier-preview-leading")).toHaveTextContent("TEXA-");
    expect(screen.getByTestId("qualifier-preview-highlighted")).toHaveTextContent("BA");
    expect(screen.getByTestId("qualifier-preview-trailing")).toHaveTextContent("-R01-001-4D5E");
  });
});
