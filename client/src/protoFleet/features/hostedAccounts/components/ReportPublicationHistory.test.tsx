import { act, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import ReportPublicationHistory from "./ReportPublicationHistory";
import { AccountReportSchema, ReportSummarySchema } from "@/protoFleet/api/generated/hosted/v1/reporting_pb";

it("keeps an in-flight history read and its results across identical polling responses", async () => {
  const user = userEvent.setup();
  const report = () =>
    create(AccountReportSchema, {
      reportId: "current",
      seriesId: "series",
      number: 2n,
      status: "issued",
      correctionOf: "original",
    });
  const publications = () => [
    create(ReportSummarySchema, {
      reportId: "current",
      seriesId: "series",
      number: 2n,
      status: "issued",
      correctionOf: "original",
    }),
  ];
  let resolve!: (value: ReturnType<typeof report>) => void;
  let requestSignal!: AbortSignal;
  const pending = new Promise<ReturnType<typeof report>>((done) => {
    resolve = done;
  });
  const loadReport = vi.fn((_id: string, signal: AbortSignal) => {
    requestSignal = signal;
    return pending;
  });
  const onSelect = vi.fn();
  const { rerender } = render(
    <ReportPublicationHistory
      report={report()}
      publications={publications()}
      loadReport={loadReport}
      onSelect={onSelect}
    />,
  );
  await user.click(screen.getByText("Publication history"));
  await waitFor(() => expect(loadReport).toHaveBeenCalledTimes(1));
  const originalSignal = requestSignal;
  rerender(
    <ReportPublicationHistory
      report={report()}
      publications={publications()}
      loadReport={loadReport}
      onSelect={onSelect}
    />,
  );
  expect(originalSignal.aborted).toBe(false);
  expect(loadReport).toHaveBeenCalledTimes(1);
  await act(async () =>
    resolve(create(AccountReportSchema, { reportId: "original", seriesId: "series", number: 1n, status: "issued" })),
  );
  await screen.findByText("Replaced");
  rerender(
    <ReportPublicationHistory
      report={report()}
      publications={publications()}
      loadReport={loadReport}
      onSelect={onSelect}
    />,
  );
  expect(screen.getByText("Current publication")).toBeInTheDocument();
  expect(loadReport).toHaveBeenCalledTimes(1);
});
