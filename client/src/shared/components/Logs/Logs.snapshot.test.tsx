import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeAll, describe, expect, it, vi } from "vitest";

import Logs, { type LogsData } from "./Logs";

const downloadLogs = vi.hoisted(() => vi.fn());
vi.mock("./utility", async (importOriginal) => ({ ...(await importOriginal<object>()), downloadLogs }));

const snapshot = (...messages: string[]): LogsData => ({
  kind: "structured",
  snapshot: true,
  entries: messages.map((message, index) => ({
    id: BigInt(index),
    timestamp: "2026-11-13 10:00:00",
    level: "info",
    message,
  })),
  lines: messages.length,
});

const noExport = async () => undefined;

describe("Logs snapshot mode", () => {
  beforeAll(() => {
    // jsdom has no layout, so scrolling the log pane is a no-op.
    Element.prototype.scrollIntoView = () => undefined;
  });

  it("replaces the view with each snapshot instead of appending to it", () => {
    const { rerender } = render(<Logs fetchMaxLogs={noExport} logsData={snapshot("pool connected", "fan ramped")} />);
    expect(screen.getByText(/pool connected/)).toBeInTheDocument();

    rerender(<Logs fetchMaxLogs={noExport} logsData={snapshot("breaker opened")} />);

    expect(screen.getByText(/breaker opened/)).toBeInTheDocument();
    expect(screen.queryByText(/pool connected/)).not.toBeInTheDocument();
    expect(screen.queryByText(/fan ramped/)).not.toBeInTheDocument();
  });

  it("shows an empty state when the snapshot has no entries", () => {
    const { rerender } = render(<Logs fetchMaxLogs={noExport} logsData={snapshot("pool connected")} />);

    rerender(<Logs fetchMaxLogs={noExport} logsData={snapshot()} />);

    expect(screen.queryByText(/pool connected/)).not.toBeInTheDocument();
    expect(screen.getByText("No log entries")).toBeInTheDocument();
  });

  it("exports the caller's own rows when it supplies them", async () => {
    const exportCsv = vi.fn(async () => ["Time,Severity", '"2026-11-13T10:00:00.123Z","info"']);
    render(<Logs exportCsv={exportCsv} logsData={snapshot("pool connected")} downloadFilename="controller-logs" />);

    fireEvent.click(screen.getByRole("button", { name: "Export" }));

    await waitFor(() =>
      expect(downloadLogs).toHaveBeenCalledWith(
        ["Time,Severity", '"2026-11-13T10:00:00.123Z","info"'],
        expect.stringMatching(/^controller-logs.*\.csv$/),
      ),
    );
  });
});
