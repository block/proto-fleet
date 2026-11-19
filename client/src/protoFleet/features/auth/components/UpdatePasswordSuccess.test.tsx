import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UpdatePasswordSuccess } from "./UpdatePasswordSuccess";

const mockOnLogin = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
});

describe("UpdatePasswordSuccess", () => {
  it("renders success message", () => {
    const { getByText } = render(<UpdatePasswordSuccess onLogin={mockOnLogin} />);

    expect(getByText("Password saved")).toBeInTheDocument();
    expect(getByText("Password updated.")).toBeInTheDocument();
  });

  it("renders Logo component", () => {
    const { container } = render(<UpdatePasswordSuccess onLogin={mockOnLogin} />);

    const logo = container.querySelector("svg");
    expect(logo).toBeTruthy();
  });

  it("renders Footer component", () => {
    const { container } = render(<UpdatePasswordSuccess onLogin={mockOnLogin} />);

    expect(container.querySelector("footer")).toBeTruthy();
  });

  it("uses the heading, description and layout classes", () => {
    const { container, getByText } = render(<UpdatePasswordSuccess onLogin={mockOnLogin} />);

    expect(getByText("Password saved").className).toContain("text-heading-300");
    expect(getByText("Password updated.").className).toContain("text-300");
    expect(container.querySelector(".h-screen")).toBeTruthy();
    expect(container.querySelector(".max-w-100")).toBeTruthy();
  });

  it("calls onLogin when button is clicked", () => {
    const { getByText } = render(<UpdatePasswordSuccess onLogin={mockOnLogin} />);

    const loginButton = getByText("Login");

    fireEvent.click(loginButton);
    expect(mockOnLogin).toHaveBeenCalledTimes(1);
  });
});
