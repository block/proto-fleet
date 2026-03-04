import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import NamePreview from "./NamePreview";

vi.mock("@/shared/assets/icons", () => ({
  ArrowDown: vi.fn(() => <span data-testid="arrow-icon" />),
}));

describe("NamePreview", () => {
  it("shows only the current name when newName equals currentName", () => {
    render(<NamePreview currentName="Bitmain S17" newName="Bitmain S17" />);

    expect(screen.getByText("Bitmain S17")).toBeInTheDocument();
    expect(screen.queryByTestId("arrow-icon")).not.toBeInTheDocument();
  });

  it("shows current → new transition when newName differs", () => {
    render(<NamePreview currentName="Bitmain S17" newName="My Miner" />);

    expect(screen.getByTestId("arrow-icon")).toBeInTheDocument();
    expect(screen.getByText("Bitmain S17")).toBeInTheDocument();
    expect(screen.getByText("My Miner")).toBeInTheDocument();
  });

  it("shows em dash when newName is empty", () => {
    render(<NamePreview currentName="Bitmain S17" newName="" />);

    expect(screen.getByTestId("arrow-icon")).toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("shows em dash when newName is whitespace only", () => {
    render(<NamePreview currentName="Bitmain S17" newName="   " />);

    expect(screen.getByTestId("arrow-icon")).toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("shows no transition when newName matches currentName after trimming", () => {
    render(<NamePreview currentName="Bitmain S17" newName="Bitmain S17   " />);

    expect(screen.queryByTestId("arrow-icon")).not.toBeInTheDocument();
    expect(screen.getByText("Bitmain S17")).toBeInTheDocument();
  });
});
