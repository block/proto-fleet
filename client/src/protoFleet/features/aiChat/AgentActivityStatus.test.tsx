import { render, screen } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import AgentActivityStatus from "./AgentActivityStatus";
import type { AgentActivity } from "./types";

const activity = (status: AgentActivity["status"], summary: string): AgentActivity => ({
  id: "activity-1",
  status,
  summary,
  timestamp: new Date("2026-07-16T12:00:00Z"),
  sequence: 1,
});

describe("AgentActivityStatus", () => {
  test("renders compact running, completed, and unsuccessful states", () => {
    const { rerender } = render(<AgentActivityStatus activity={activity("running", "Checking fleet health")} />);

    expect(screen.getByRole("img", { name: "In progress" })).toBeInTheDocument();
    expect(screen.getByText("Checking fleet health")).toBeInTheDocument();
    expect(screen.getByTestId("agent-activity-status").tagName).toBe("DIV");

    rerender(<AgentActivityStatus activity={activity("completed", "Read state for 14 miners")} />);
    expect(screen.getByRole("img", { name: "Completed" })).toBeInTheDocument();
    expect(screen.getByText("Read state for 14 miners")).toBeInTheDocument();

    rerender(<AgentActivityStatus activity={activity("failed", "Unable to read this fleet data")} />);
    expect(screen.getByRole("img", { name: "Couldn't complete" })).toBeInTheDocument();
    expect(screen.getByText("Unable to read this fleet data")).toBeInTheDocument();
  });
});
