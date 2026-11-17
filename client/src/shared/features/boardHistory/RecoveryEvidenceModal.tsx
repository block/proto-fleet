import { useCallback, useMemo, useState } from "react";

import { parseEvidenceDocument } from "./evidence";
import { causeLabel, sourceLabel, stackLabel, triggerLabel } from "./labels";
import {
  type BoardHistorySource,
  RECOVERY_EVIDENCE_PER_BOARD,
  RECOVERY_EVIDENCE_RETENTION_DAYS,
  type RecoveryEvidenceRecord,
  type RecoveryEvidenceSummary,
} from "./types";
import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { StatusLabel } from "@/shared/components/StatusCircle";
import SummaryRow from "@/shared/components/SummaryRow";
import { formatControllerTime } from "@/shared/features/controllerLogs/coverage";
import ReadNotices from "@/shared/features/controllerLogs/ReadNotices";
import { usePolledRead } from "@/shared/features/controllerLogs/usePolledRead";
import { downloadBlob, getFileName } from "@/shared/utils/utility";

type EvidenceColumn = "capturedAt" | "trigger" | "causes" | "complete";

const colTitles: ColTitles<EvidenceColumn> = {
  capturedAt: "Captured",
  trigger: "Trigger",
  causes: "Causes",
  complete: "Contents",
};

const causesText = (causes: string[]) => (causes.length ? causes.map(causeLabel).join(", ") : "—");

const colConfig: ColConfig<RecoveryEvidenceSummary, string, EvidenceColumn> = {
  capturedAt: { width: "w-[190px]", component: (summary) => formatControllerTime(summary.capturedAt) || "—" },
  trigger: { width: "w-[200px]", component: (summary) => triggerLabel(summary.trigger) },
  causes: { width: "w-[220px]", component: (summary) => causesText(summary.causes) },
  complete: {
    width: "w-[120px]",
    component: (summary) => (summary.truncated ? <StatusLabel status="warning">Truncated</StatusLabel> : "Complete"),
  },
};
const activeCols = Object.keys(colTitles) as EvidenceColumn[];
const listPadding = { phone: "0px", tablet: "0px", laptop: "0px", desktop: "0px" };
const sourceStatus = { ok: "normal", unavailable: "inactive", error: "error", unknown: "inactive" } as const;
const statusLabel = { ok: "Captured", unavailable: "Unavailable", error: "Failed", unknown: "Unknown" } as const;

const Loading = () => (
  <div className="flex h-[189px] items-center justify-center">
    <ProgressCircular indeterminate />
  </div>
);

const EvidenceDetail = ({ record }: { record: RecoveryEvidenceRecord }) => {
  const { summary } = record;
  const document = useMemo(() => parseEvidenceDocument(record.json), [record.json]);
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col">
        <SummaryRow label="Captured">{formatControllerTime(summary.capturedAt) || "—"}</SummaryRow>
        <SummaryRow label="Trigger">{triggerLabel(summary.trigger)}</SummaryRow>
        <SummaryRow label="Causes" className="break-words">
          {causesText(summary.causes)}
        </SummaryRow>
        <SummaryRow label="Captured by" className="font-mono break-all">
          {summary.capturedBy || "—"}
        </SummaryRow>
        <SummaryRow label="Episode" className="font-mono break-all">
          {summary.episodeId || "—"}
        </SummaryRow>
        <SummaryRow label="Evidence ID" className="font-mono break-all" divider={false}>
          {summary.id}
        </SummaryRow>
      </div>
      {summary.truncated || document?.omitted.length ? (
        <Callout
          intent="warning"
          prefixIcon={<Alert />}
          title="Part of this snapshot was left out"
          subtitle={
            document?.omitted.length
              ? `The controller dropped ${document.omitted.map(sourceLabel).join(", ")} to keep the snapshot within its size limit.`
              : "The controller dropped data to keep the snapshot within its size limit."
          }
          testId="recovery-evidence-truncated"
        />
      ) : null}
      {document ? (
        <>
          <div className="flex flex-col gap-2">
            <div className="text-heading-200 text-text-primary">Sources</div>
            {document.sources.length ? (
              <div className="flex flex-col" data-testid="recovery-evidence-sources">
                {document.sources.map((source, index) => (
                  <SummaryRow
                    key={source.name}
                    label={sourceLabel(source.name)}
                    divider={index < document.sources.length - 1}
                  >
                    <StatusLabel status={sourceStatus[source.status]}>
                      {[statusLabel[source.status], source.detail].filter(Boolean).join(": ")}
                    </StatusLabel>
                  </SummaryRow>
                ))}
              </div>
            ) : (
              <p className="text-200 text-text-primary-70">The snapshot names no sources.</p>
            )}
          </div>
          {document.stackDropouts.length ? (
            <div className="flex flex-col gap-2">
              <div className="text-heading-200 text-text-primary">Stack dropouts</div>
              <div className="flex flex-col">
                {document.stackDropouts.map((dropout, index) => (
                  <SummaryRow
                    key={index}
                    label={stackLabel(dropout.stack)}
                    divider={index < document.stackDropouts.length - 1}
                  >
                    {[
                      dropout.detectedAt && `Dropped ${formatControllerTime(dropout.detectedAt)}`,
                      dropout.clearedAt ? `back ${formatControllerTime(dropout.clearedAt)}` : "not back when captured",
                    ]
                      .filter(Boolean)
                      .join(", ")}
                  </SummaryRow>
                ))}
              </div>
            </div>
          ) : null}
        </>
      ) : (
        <Callout
          intent="warning"
          prefixIcon={<Alert />}
          title="Snapshot could not be read"
          subtitle="Download the record to inspect it."
          testId="recovery-evidence-unreadable"
        />
      )}
    </div>
  );
};

interface RecoveryEvidenceModalProps extends Required<Pick<BoardHistorySource, "listEvidence" | "readEvidence">> {
  boardLabel: string;
  onDismiss: () => void;
}

/** Lists one board's retained recovery evidence and opens a snapshot's summary. */
const RecoveryEvidenceModal = ({ listEvidence, readEvidence, boardLabel, onDismiss }: RecoveryEvidenceModalProps) => {
  const [selectedId, setSelectedId] = useState<string>();
  // One list read per open, keyed by the board.
  const readList = useCallback((_: string, signal: AbortSignal) => listEvidence(undefined, signal), [listEvidence]);
  const list = usePolledRead(boardLabel, readList);
  const detail = usePolledRead(selectedId ?? "", selectedId ? readEvidence : undefined);
  const record = detail.data;

  const buttons = selectedId
    ? [
        { text: "Back", variant: "secondary" as const, onClick: () => setSelectedId(undefined) },
        {
          text: "Download record (JSON)",
          variant: "primary" as const,
          disabled: !record,
          onClick: () =>
            record &&
            downloadBlob(
              new Blob([record.json], { type: "application/json" }),
              getFileName(`recovery-evidence-${boardLabel}-${record.summary.id}`.replaceAll(":", ""), "json"),
            ),
        },
      ]
    : undefined;

  return (
    <Modal
      title={selectedId ? "Recovery evidence snapshot" : "Recovery evidence"}
      dialogLabel="Recovery evidence"
      description={
        selectedId
          ? undefined
          : `The controller keeps evidence for ${RECOVERY_EVIDENCE_RETENTION_DAYS} days, up to ${RECOVERY_EVIDENCE_PER_BOARD} snapshots per board. It is a record of past recoveries, not the board's current state.`
      }
      size="large"
      manageFocus
      buttons={buttons}
      onDismiss={onDismiss}
    >
      {selectedId ? (
        <>
          <ReadNotices
            staleTitle="Snapshot unavailable"
            unavailableTitle="Snapshot unavailable"
            error={detail.error}
            testIdPrefix="recovery-evidence-detail"
            className="mb-3"
          />
          {detail.loading ? <Loading /> : record ? <EvidenceDetail record={record} /> : null}
        </>
      ) : (
        <>
          <ReadNotices
            staleTitle="Recovery evidence unavailable"
            unavailableTitle="Recovery evidence unavailable"
            error={list.error}
            testIdPrefix="recovery-evidence"
            className="mb-3"
          />
          {list.loading ? (
            <Loading />
          ) : list.data ? (
            <List
              activeCols={activeCols}
              colTitles={colTitles}
              colConfig={colConfig}
              items={list.data}
              itemKey="id"
              itemName={{ singular: "snapshot", plural: "snapshots" }}
              total={list.data.length}
              paddingLeft={listPadding}
              paddingRight={listPadding}
              onRowClick={(summary) => setSelectedId(summary.id)}
              noDataElement={
                <div className="flex h-[189px] w-full items-center justify-center rounded-2xl bg-core-primary-5">
                  <div className="text-heading-100 text-text-primary-50">No retained recovery evidence</div>
                </div>
              }
            />
          ) : null}
        </>
      )}
    </Modal>
  );
};

export default RecoveryEvidenceModal;
