import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import SiteMapCsvImportModal from "./SiteMapCsvImportModal";
import {
  ImportChangeSummarySchema,
  ImportOperation,
  type ImportSiteMapCsvResponse,
  ImportSiteMapCsvResponseSchema,
  ImportValidationErrorSchema,
  OmissionMode,
} from "@/protoFleet/api/generated/sitemap/v1/sitemap_pb";

const { mockImportSiteMapCsv, mockPushToast } = vi.hoisted(() => ({
  mockImportSiteMapCsv: vi.fn(),
  mockPushToast: vi.fn(),
}));

vi.mock("@/protoFleet/api/useSiteMapCsv", () => ({
  default: () => ({
    importSiteMapCsv: mockImportSiteMapCsv,
    isImportingSiteMapCsv: false,
  }),
}));

vi.mock(import("@/shared/features/toaster"), async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    pushToast: mockPushToast,
  };
});

describe("SiteMapCsvImportModal", () => {
  beforeEach(() => {
    mockImportSiteMapCsv.mockReset();
    mockPushToast.mockReset();
  });

  it("keeps commit-time validation errors visible instead of reporting success", async () => {
    const onDismiss = vi.fn();
    const onImported = vi.fn();
    mockImportSiteMapCsv
      .mockResolvedValueOnce(
        create(ImportSiteMapCsvResponseSchema, {
          changes: [
            create(ImportChangeSummarySchema, {
              operation: ImportOperation.MOVE,
              entityType: "miner",
              count: 1,
              description: "miner placement rows with changed site, building, rack, or slot",
            }),
          ],
          commitToken: "preview-token",
        }),
      )
      .mockResolvedValueOnce(
        create(ImportSiteMapCsvResponseSchema, {
          errors: [
            create(ImportValidationErrorSchema, {
              row: 21,
              section: "MINER",
              message: "rack slot already occupied by miner hidden-1",
            }),
          ],
        }),
      );

    render(<SiteMapCsvImportModal open onDismiss={onDismiss} onImported={onImported} />);

    const file = new File(["csv"], "site-map.csv", { type: "text/csv" });
    fireEvent.change(screen.getByTestId("site-map-csv-file-input"), { target: { files: [file] } });

    await screen.findByText("miner placement rows with changed site, building, rack, or slot");
    fireEvent.click(screen.getByTestId("confirm-site-map-import-button"));

    await screen.findByText(/rack slot already occupied by miner hidden-1/);
    expect(mockImportSiteMapCsv).toHaveBeenLastCalledWith({
      file,
      omissionMode: OmissionMode.UNSPECIFIED,
      dryRun: false,
      commitToken: "preview-token",
    });
    expect(mockPushToast).not.toHaveBeenCalled();
    expect(onImported).not.toHaveBeenCalled();
    expect(onDismiss).not.toHaveBeenCalled();
  });

  it("does not dismiss while the commit request is in flight", async () => {
    const onDismiss = vi.fn();
    let resolveCommit: (value: ImportSiteMapCsvResponse) => void = () => {};
    const commitPromise = new Promise<ImportSiteMapCsvResponse>((resolve) => {
      resolveCommit = resolve;
    });
    mockImportSiteMapCsv
      .mockResolvedValueOnce(
        create(ImportSiteMapCsvResponseSchema, {
          changes: [
            create(ImportChangeSummarySchema, {
              operation: ImportOperation.MOVE,
              entityType: "miner",
              count: 1,
              description: "miner placement rows with changed site, building, rack, or slot",
            }),
          ],
          commitToken: "preview-token",
        }),
      )
      .mockReturnValueOnce(commitPromise);

    render(<SiteMapCsvImportModal open onDismiss={onDismiss} />);

    fireEvent.change(screen.getByTestId("site-map-csv-file-input"), {
      target: { files: [new File(["csv"], "site-map.csv", { type: "text/csv" })] },
    });

    await screen.findByText("miner placement rows with changed site, building, rack, or slot");
    fireEvent.click(screen.getByTestId("confirm-site-map-import-button"));
    const cancelButton = screen.getByText("Cancel").closest("button");
    await waitFor(() => expect(cancelButton).toBeDisabled());

    fireEvent.click(cancelButton!);
    expect(onDismiss).not.toHaveBeenCalled();

    resolveCommit(create(ImportSiteMapCsvResponseSchema));
    await waitFor(() => expect(onDismiss).toHaveBeenCalledTimes(1));
  });
});
