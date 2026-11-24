import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import AuditEventDetailModal from "./AuditEventDetailModal";
import type { AuditEvent } from "@/shared/features/controllerLogs/types";

const event: AuditEvent = {
  id: "7",
  time: "2026-11-13T10:00:00Z",
  actor: "operator@bridge",
  source: "rest:10.0.0.2:443",
  node: "ct1-a",
  action: "SetBreaker",
  target: "breaker-1",
  result: "error",
  error: "interlock open",
  details: '{"state":"open"}',
};

describe("AuditEventDetailModal", () => {
  it("shows the event's attribution, result and formatted details", () => {
    render(<AuditEventDetailModal event={event} onDismiss={vi.fn()} />);

    expect(screen.getByRole("dialog", { name: "SetBreaker" })).toBeInTheDocument();
    for (const value of ["operator@bridge", "rest:10.0.0.2:443", "ct1-a", "breaker-1", "Failed", "interlock open"]) {
      expect(screen.getByText(value)).toBeInTheDocument();
    }
    expect(screen.getByText(/"state": "open"/)).toBeInTheDocument();
  });

  it("shows details that are not JSON as recorded and omits empty sections", () => {
    render(
      <AuditEventDetailModal
        event={{ ...event, result: "ok", error: undefined, details: "raw text" }}
        onDismiss={vi.fn()}
      />,
    );

    expect(screen.getByText("Succeeded")).toBeInTheDocument();
    expect(screen.getByText("raw text")).toBeInTheDocument();
    expect(screen.queryByText("Issue")).not.toBeInTheDocument();
  });

  it("renders nothing without an event", () => {
    const { container } = render(<AuditEventDetailModal event={null} onDismiss={vi.fn()} />);
    expect(container).toBeEmptyDOMElement();
  });
});
