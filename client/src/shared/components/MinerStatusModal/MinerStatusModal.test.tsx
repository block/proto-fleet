import { render, screen } from "@testing-library/react";
import { describe, expect, test } from "vitest";
import MinerStatusModal from "./MinerStatusModal";
import { statuses } from "@/shared/components/StatusCircle";

describe("MinerStatusModal", () => {
  const mockStatusNoIssues = {
    title: "All systems are operational",
    circle: statuses.normal,
    hasIssues: false,
    issues: {
      fans: [],
      hashboards: [],
      psus: [],
      controlBoard: [],
    },
  };

  const mockStatusWithFanIssue = {
    title: "Fan not detected",
    circle: statuses.error,
    hasIssues: true,
    issues: {
      fans: [
        {
          title: "Fan 2",
          message: "Fan 2 not detected",
        },
      ],
      hashboards: [],
      psus: [],
      controlBoard: [],
    },
  };

  const mockStatusMultipleIssues = {
    title: "Multiple Issues",
    circle: statuses.error,
    hasIssues: true,
    issues: {
      fans: [
        {
          title: "Fan 1",
          message: "Fan 1 not spinning",
        },
        {
          title: "Fan 2",
          message: "Fan 2 not spinning",
        },
      ],
      hashboards: [
        {
          title: "Hashboard 1",
          message: "Hashboard 1 overheating",
        },
      ],
      psus: [],
      controlBoard: [],
    },
  };

  test("renders correctly with no issues", () => {
    render(
      <MinerStatusModal onDismiss={() => {}} status={mockStatusNoIssues} />,
    );

    expect(screen.getByText("All systems are operational")).toBeInTheDocument();
    expect(screen.getByText("Fan")).toBeInTheDocument();
    expect(screen.getByText("Hashboard")).toBeInTheDocument();
    expect(screen.getByText("Control board")).toBeInTheDocument();
    expect(screen.getByText("Power supply")).toBeInTheDocument();
  });

  test("renders correctly with a single fan issue", () => {
    render(
      <MinerStatusModal onDismiss={() => {}} status={mockStatusWithFanIssue} />,
    );

    expect(screen.getByText("Fan not detected")).toBeInTheDocument();
    expect(screen.getByText("Fan 2 not detected")).toBeInTheDocument();
  });

  test("renders correctly with multiple issues", () => {
    render(
      <MinerStatusModal
        onDismiss={() => {}}
        status={mockStatusMultipleIssues}
      />,
    );

    expect(screen.getByText("Multiple Issues")).toBeInTheDocument();
    expect(screen.getByText("Fan 1 not spinning")).toBeInTheDocument();
    expect(screen.getByText("Fan 2 not spinning")).toBeInTheDocument();
    expect(screen.getByText("Hashboard 1 overheating")).toBeInTheDocument();
  });

  test("shows done button", () => {
    render(
      <MinerStatusModal onDismiss={() => {}} status={mockStatusNoIssues} />,
    );

    const doneButton = screen.getByText("Done");
    expect(doneButton).toBeInTheDocument();
  });
});
