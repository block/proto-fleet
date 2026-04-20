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

  it("renders Login button", () => {
    const { getByText } = render(<UpdatePasswordSuccess onLogin={mockOnLogin} />);

    expect(getByText("Login")).toBeInTheDocument();
  });

  it("calls onLogin when Login button is clicked", () => {
    const { getByText } = render(<UpdatePasswordSuccess onLogin={mockOnLogin} />);

    fireEvent.click(getByText("Login"));

    expect(mockOnLogin).toHaveBeenCalled();
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

  it("uses correct heading size", () => {
    const { getByText } = render(<UpdatePasswordSuccess onLogin={mockOnLogin} />);

    const heading = getByText("Password saved");
    expect(heading.className).toContain("text-heading-300");
  });

  it("button has primary variant styling", () => {
    const { getByText } = render(<UpdatePasswordSuccess onLogin={mockOnLogin} />);

    const button = getByText("Login");
    expect(button).toBeInTheDocument();
  });

  it("renders with proper layout structure", () => {
    const { container } = render(<UpdatePasswordSuccess onLogin={mockOnLogin} />);

    const mainContainer = container.querySelector(".h-screen");
    expect(mainContainer).toBeTruthy();

    const contentWrapper = container.querySelector(".max-w-100");
    expect(contentWrapper).toBeTruthy();
  });

  it("calls onLogin when button is clicked", () => {
    const { getByText } = render(<UpdatePasswordSuccess onLogin={mockOnLogin} />);

    const loginButton = getByText("Login");

    fireEvent.click(loginButton);
    expect(mockOnLogin).toHaveBeenCalledTimes(1);
  });

  it("renders description text with correct styling", () => {
    const { getByText } = render(<UpdatePasswordSuccess onLogin={mockOnLogin} />);

    const description = getByText("Password updated.");
    expect(description.className).toContain("text-300");
  });
});
