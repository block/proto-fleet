import React from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import FixedValueOptionsModal from "./FixedValueOptionsModal";

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
        <div data-testid="fixed-value-options-modal">
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

describe("FixedValueOptionsModal", () => {
  const onConfirm = vi.fn();
  const onDismiss = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("hides string section options when character count is All", () => {
    render(
      <FixedValueOptionsModal
        open={true}
        previewName="M-001"
        initialValues={{ characterCount: "all" }}
        onConfirm={onConfirm}
        onDismiss={onDismiss}
      />,
    );

    expect(screen.queryByTestId("fixed-value-string-section-option-first")).not.toBeInTheDocument();
    expect(screen.queryByTestId("fixed-value-string-section-option-last")).not.toBeInTheDocument();
  });

  it("defaults to last 3 characters", () => {
    render(<FixedValueOptionsModal open={true} previewName="M-001" onConfirm={onConfirm} onDismiss={onDismiss} />);

    expect(screen.getByTestId("fixed-value-string-section-option-first")).toHaveTextContent("First 3 characters");
    expect(screen.getByTestId("fixed-value-string-section-option-last")).toHaveTextContent("Last 3 characters");
  });

  it("submits selected count and section", () => {
    render(<FixedValueOptionsModal open={true} previewName="M-001" onConfirm={onConfirm} onDismiss={onDismiss} />);

    fireEvent.click(screen.getByTestId("fixed-value-character-count-option-3"));
    fireEvent.click(screen.getByTestId("fixed-value-string-section-option-last"));
    fireEvent.click(screen.getByTestId("fixed-value-options-save-button"));

    expect(onConfirm).toHaveBeenCalledWith({
      characterCount: 3,
      stringSection: "last",
    });
  });

  it("highlights entire preview name when no highlightedText is provided", () => {
    render(
      <FixedValueOptionsModal
        open={true}
        previewName="TEXA-BA-R01-001-4D5E"
        initialValues={{ characterCount: 4, stringSection: "first" }}
        onConfirm={onConfirm}
        onDismiss={onDismiss}
      />,
    );

    expect(screen.getByTestId("fixed-value-preview-leading")).toHaveTextContent("");
    expect(screen.getByTestId("fixed-value-preview-highlighted")).toHaveTextContent("TEXA-BA-R01-001-4D5E");
    expect(screen.getByTestId("fixed-value-preview-trailing")).toHaveTextContent("");
  });

  it("highlights the specified text in preview", () => {
    render(
      <FixedValueOptionsModal
        open={true}
        previewName="TEXA-BA-R01-001-4D5E"
        highlightedText="BA"
        onConfirm={onConfirm}
        onDismiss={onDismiss}
      />,
    );

    expect(screen.getByTestId("fixed-value-preview-leading")).toHaveTextContent("TEXA-");
    expect(screen.getByTestId("fixed-value-preview-highlighted")).toHaveTextContent("BA");
    expect(screen.getByTestId("fixed-value-preview-trailing")).toHaveTextContent("-R01-001-4D5E");
  });
});
