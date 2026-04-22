import { type ReactNode } from "react";
import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import Header from ".";
import { variants } from "@/shared/components/Button";

vi.mock("@/shared/components/Button", async () => {
  const actual = await vi.importActual<typeof import("@/shared/components/Button")>("@/shared/components/Button");

  return {
    ...actual,
    default: ({
      ariaLabel,
      className,
      onClick,
      prefixIcon,
      testId,
    }: {
      ariaLabel?: string;
      className?: string;
      onClick?: () => void;
      prefixIcon?: ReactNode;
      testId?: string;
    }) => (
      <button aria-label={ariaLabel} className={className} data-testid={testId} onClick={onClick} type="button">
        {prefixIcon}
      </button>
    ),
  };
});

describe("Header", () => {
  it("passes iconButtonClassName to the shared icon button wrapper", () => {
    const { getByTestId } = render(
      <Header
        icon={<div>icon</div>}
        iconAriaLabel="Open header action"
        iconOnClick={vi.fn()}
        iconButtonClassName="!p-0"
        title="Rename miners"
        inline
      />,
    );

    expect(getByTestId("header-icon-button")).toHaveClass("!p-0");
  });

  it("passes iconAriaLabel to the shared icon button wrapper", () => {
    const { getByTestId } = render(
      <Header icon={<div>icon</div>} iconAriaLabel="Close header" iconOnClick={vi.fn()} title="Rename miners" inline />,
    );

    expect(getByTestId("header-icon-button")).toHaveAttribute("aria-label", "Close header");
  });

  it("passes buttonsWrapperClassName to the button group wrapper", () => {
    const { container } = render(
      <Header
        title="Rename miners"
        buttons={[{ text: "Save", onClick: vi.fn(), variant: variants.primary }]}
        buttonsWrapperClassName="phone:hidden"
      />,
    );

    expect(container.querySelector(".phone\\:hidden")).not.toBeNull();
  });
});
