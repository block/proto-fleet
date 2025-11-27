import { BrowserRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import ComponentErrors from "./ComponentErrors";

describe("ComponentErrors", () => {
  it("renders heading with no issues when errorCount is 0", () => {
    render(<ComponentErrors icon={<div data-testid="icon">Icon</div>} heading="Control Boards" errorCount={0} />);

    expect(screen.getByText("Control Boards")).toBeInTheDocument();
    expect(screen.getByText("No issues")).toBeInTheDocument();
  });

  it("renders icon correctly", () => {
    render(
      <ComponentErrors icon={<div data-testid="test-icon">Test Icon</div>} heading="Test Heading" errorCount={0} />,
    );

    expect(screen.getByTestId("test-icon")).toBeInTheDocument();
    expect(screen.getByText("Test Icon")).toBeInTheDocument();
  });

  it("applies custom className", () => {
    const { container } = render(
      <ComponentErrors icon={<div>Icon</div>} heading="Test" errorCount={0} className="custom-class" />,
    );

    const componentErrors = container.firstChild as HTMLElement;
    expect(componentErrors).toHaveClass("custom-class");
  });

  it("renders correct message for single miner error", () => {
    render(<ComponentErrors icon={<div>Icon</div>} heading="Fans" errorCount={1} />);

    expect(screen.getByText("Fans")).toBeInTheDocument();
    expect(screen.getByText("1 miner needs attention")).toBeInTheDocument();
  });

  it("renders correct message for multiple miner errors", () => {
    render(<ComponentErrors icon={<div>Icon</div>} heading="Hashboards" errorCount={5} />);

    expect(screen.getByText("Hashboards")).toBeInTheDocument();
    expect(screen.getByText("5 miners need attention")).toBeInTheDocument();
  });

  it("renders skeleton loader when errorCount is undefined", () => {
    render(<ComponentErrors icon={<div>Icon</div>} heading="Control Boards" errorCount={undefined} />);

    expect(screen.getByText("Control Boards")).toBeInTheDocument();
    expect(screen.getByTestId("skeleton-bar")).toBeInTheDocument();
  });

  it("renders as a div when href is not provided", () => {
    const { container } = render(<ComponentErrors icon={<div>Icon</div>} heading="Control Boards" errorCount={0} />);

    const element = container.firstChild as HTMLElement;
    expect(element.tagName).toBe("DIV");
  });

  it("renders as a Link when href is provided", () => {
    render(
      <BrowserRouter>
        <ComponentErrors icon={<div>Icon</div>} heading="Control Boards" errorCount={2} href="/errors/control-boards" />
      </BrowserRouter>,
    );

    const link = screen.getByRole("link");
    expect(link).toBeInTheDocument();
    expect(link).toHaveAttribute("href", "/errors/control-boards");
  });
});
