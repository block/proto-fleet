import { type ReactNode, useCallback, useEffect, useRef, useState } from "react";
import { create, fromJson, type JsonValue, toJson } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import { ACCOUNT_READ_TIMEOUT_MS, hostedAccountsClient } from "../api";
import { reportLabel } from "../reportFormat";
import { type ReportingImportProgress, runReportingImport } from "../reportingImports";
import {
  importReportingRows,
  MAX_REPORTING_IMPORT_BYTES,
  reportingColumns,
  reportingFactID,
  reportingFactJSON,
  reportingFieldValue,
  reportingKinds,
  type ReportingRegister,
  reportingRegisters,
  reportingTemplate,
  reportingUpsert,
} from "../reportingInputs";
import { useAccountQuery } from "../useAccountQuery";
import { AccountState } from "./AccountState";
import { ReportingError } from "./ReportingError";
import { ReportingForm } from "./ReportingForm";
import { ReportingRows } from "./ReportingRows";
import {
  type HostReportingSetup,
  type ReportingFact,
  ReportingFactMutationSchema,
  ReportingFactSchema,
} from "@/protoFleet/api/generated/hosted/v1/reporting_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import ListPagination from "@/protoFleet/components/ListPagination";
import { useFleetStore } from "@/protoFleet/store";
import Button, { sizes } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";
import { downloadBlob } from "@/shared/utils/utility";

type Props = { setup: HostReportingSetup; signal: AbortSignal; onChanged: () => void };
type RetainedImport = { register: ReportingRegister; sourceRef: string; progress: ReportingImportProgress };
const retainedImports = new Map<string, RetainedImport>();
const recoveryPrefix = "proto-fleet-reporting-import:";
const readRetainedImport = (account: string): RetainedImport | null => {
  const key = importKey(account);
  const cached = retainedImports.get(key);
  if (cached) return cached;
  try {
    const raw = sessionStorage.getItem(recoveryPrefix + key);
    if (!raw) return null;
    const saved = JSON.parse(raw);
    if (
      !reportingRegisters.includes(saved.register) ||
      !Array.isArray(saved.progress.pending) ||
      saved.progress.pending.length > 200
    )
      return null;
    const retained = {
      ...saved,
      progress: {
        ...saved.progress,
        revision: BigInt(saved.progress.revision),
        pending: saved.progress.pending.map((fact: JsonValue) => fromJson(ReportingFactSchema, fact)),
      },
    } as RetainedImport;
    retainedImports.set(key, retained);
    return retained;
  } catch {
    return null;
  }
};
const persistRetainedImport = (key: string, value: RetainedImport, required = false) => {
  retainedImports.set(key, value);
  try {
    sessionStorage.setItem(
      recoveryPrefix + key,
      JSON.stringify({
        ...value,
        progress: {
          ...value.progress,
          revision: value.progress.revision.toString(),
          pending: value.progress.pending.map((fact) => toJson(ReportingFactSchema, fact)),
        },
      }),
    );
  } catch {
    if (required)
      throw new ConnectError(
        "The browser could not retain the submitted rows for recovery. Free session storage before importing.",
        Code.ResourceExhausted,
      );
  }
};
const forgetRetainedImport = (account: string) => {
  const key = importKey(account);
  retainedImports.delete(key);
  try {
    sessionStorage.removeItem(recoveryPrefix + key);
  } catch {
    /* The session-scoped entry is never replayed automatically. */
  }
};
let retainedGeneration = -1;
const importKey = (account: string) => {
  const generation = useFleetStore.getState().auth.sessionGeneration;
  if (generation !== retainedGeneration) {
    retainedImports.clear();
    retainedGeneration = generation;
  }
  return `${generation}:${useFleetStore.getState().auth.username}:${account}`;
};

const FactDetails = ({ facts, actions }: { facts: ReportingFact[]; actions?: (fact: ReportingFact) => ReactNode }) => (
  <>
    {facts.map((fact) => (
      <div key={reportingFactID(fact)} className="border-b border-border-20 py-3">
        <dl className="grid grid-cols-1 gap-x-4 gap-y-1 sm:grid-cols-[auto_minmax(0,1fr)]">
          {Object.entries(reportingFactJSON(fact)).map(([key, value]) => (
            <div key={key} className="contents">
              <dt>{reportLabel(key)}</dt>
              <dd className="break-words">{Array.isArray(value) ? value.join("; ") : String(value)}</dd>
            </div>
          ))}
        </dl>
        {actions?.(fact)}
      </div>
    ))}
  </>
);

const FactEditor = ({
  register,
  fact,
  revision,
  setup,
  signal,
  onChanged,
  onDismiss,
  deleting = false,
}: {
  register: ReportingRegister;
  fact?: ReportingFact;
  revision: bigint;
  deleting?: boolean;
  onDismiss: () => void;
} & Props) => {
  const original = fact ? reportingFactJSON(fact) : {};
  const [fields, setFields] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      reportingColumns[register]
        .split(",")
        .map((key) => [
          key,
          Array.isArray(original[key]) ? (original[key] as string[]).join(";") : String(original[key] ?? ""),
        ]),
    ),
  );
  const [sourceRef, setSourceRef] = useState("");
  const title = deleting ? "Delete fact" : fact ? "Edit fact" : "Add fact";
  return (
    <ReportingForm
      title={title}
      confirm={deleting ? "Delete fact" : "Save fact"}
      signal={signal}
      valid={Boolean(sourceRef.trim() && (deleting || fields.id?.trim()))}
      onDismiss={onDismiss}
      onConflict={onChanged}
      onSubmit={async (requestSignal) => {
        const mutation = deleting
          ? create(ReportingFactMutationSchema, {
              kind: reportingKinds[register],
              id: reportingFactID(fact!),
              change: { case: "delete", value: true },
            })
          : reportingUpsert(
              register,
              importReportingRows(
                register,
                JSON.stringify([
                  Object.fromEntries(
                    Object.entries(fields)
                      .filter(([, v]) => v !== "")
                      .map(([key, value]) => [key, reportingFieldValue(key, value)]),
                  ),
                ]),
                "json",
              )[0],
            );
        await hostedAccountsClient.mutateHostReportingInputs(
          {
            accountId: setup.accountId,
            expectedRevision: revision,
            sourceRef: sourceRef.trim(),
            mutations: [mutation],
          },
          { signal: requestSignal },
        );
        if (!requestSignal.aborted) onChanged();
      }}
    >
      {deleting ? (
        <p>
          Delete {reportLabel(register)} fact “{reportingFactID(fact!)}”?
        </p>
      ) : (
        <>
          <p>Times use ISO 8601 with an offset. Decimals stay exact text.</p>
          {reportingColumns[register].split(",").map((key) =>
            ["wholeModule", "complete", "qualified"].includes(key) ? (
              <Select
                key={key}
                id={`fact-${key}`}
                label={reportLabel(key)}
                value={fields[key]}
                options={[
                  { value: "", label: "Unspecified" },
                  { value: "true", label: "True" },
                  { value: "false", label: "False" },
                ]}
                onChange={(value) => setFields({ ...fields, [key]: value })}
              />
            ) : (
              <Input
                key={key}
                id={`fact-${key}`}
                label={reportLabel(key)}
                initValue={fields[key]}
                disabled={Boolean(fact && key === "id")}
                onChange={(value) => setFields({ ...fields, [key]: value })}
              />
            ),
          )}
        </>
      )}
      <Input
        id="fact-change-evidence"
        label="Change evidence reference"
        initValue={sourceRef}
        onChange={setSourceRef}
      />
    </ReportingForm>
  );
};

const ImportFacts = ({
  register,
  revision,
  setup,
  signal,
  onChanged,
  onDismiss,
  onProgress,
}: {
  register: ReportingRegister;
  revision: bigint;
  onDismiss: () => void;
  onProgress: (status: RetainedImport) => void;
} & Props) => {
  const [facts, setFacts] = useState<ReportingFact[]>([]);
  const [sourceRef, setSourceRef] = useState("");
  const [error, setError] = useState("");
  const [progress, setProgress] = useState<ReportingImportProgress | null>(null);
  const [busy, setBusy] = useState(false);
  const stop = useRef(false);
  const submitting = useRef(false);
  const version = useRef(0);
  const lifetime = useRef<AbortController | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    return () => {
      controller.abort();
    };
  }, []);
  const submit = async () => {
    if (submitting.current || !facts.length || !sourceRef.trim() || !lifetime.current) return;
    submitting.current = true;
    stop.current = false;
    setBusy(true);
    setError("");
    const requestSignal = AbortSignal.any([signal, lifetime.current.signal]);
    const generation = useFleetStore.getState().auth.sessionGeneration;
    const key = importKey(setup.accountId);
    const result = await runReportingImport(
      register,
      facts,
      revision,
      async (batch, expected) => {
        const pending = retainedImports.get(key);
        if (pending) persistRetainedImport(key, pending, true);
        const response = await hostedAccountsClient.mutateHostReportingInputs(
          {
            accountId: setup.accountId,
            expectedRevision: expected,
            sourceRef: sourceRef.trim(),
            mutations: batch.map((fact) => reportingUpsert(register, fact)),
          },
          { signal: requestSignal },
        );
        if (!response.input) throw new Error("The server did not confirm the new input revision.");
        return response.input.revision;
      },
      () => stop.current || requestSignal.aborted || generation !== useFleetStore.getState().auth.sessionGeneration,
      (next) => {
        if (generation !== useFleetStore.getState().auth.sessionGeneration) return;
        const status = {
          register,
          sourceRef: sourceRef.trim(),
          progress: next.pending.length ? { ...next, status: "unknown" as const } : next,
        };
        persistRetainedImport(key, status);
        if (!requestSignal.aborted) {
          setProgress(next);
          onProgress({ register, sourceRef: sourceRef.trim(), progress: next });
        }
      },
    );
    submitting.current = false;
    if (!requestSignal.aborted && generation === useFleetStore.getState().auth.sessionGeneration) {
      setBusy(false);
      setProgress(result);
      onChanged();
    }
  };
  const finished = progress !== null && progress.status !== "running";
  return (
    <Modal
      open
      title="Add or update reporting facts"
      dialogLabel="Add or update reporting facts"
      manageFocus
      onDismiss={busy ? undefined : onDismiss}
      buttons={[
        {
          text: busy ? "Cancel import" : finished ? "Done" : "Cancel",
          variant: "secondary",
          onClick: () => {
            if (busy) {
              stop.current = true;
              setError("Stopping after the submitted batch returns.");
            } else onDismiss();
          },
          dismissModalOnClick: false,
        },
        ...(!finished
          ? [
              {
                text: "Add or update rows",
                variant: "primary" as const,
                disabled: busy || !facts.length || !sourceRef.trim(),
                loading: busy,
                onClick: submit,
                dismissModalOnClick: false,
              },
            ]
          : []),
      ]}
    >
      <p>
        {reportLabel(register)} rows are added or updated by ID. Rows missing from this file are kept. Each batch
        commits separately.
      </p>
      <Button
        variant="secondary"
        text="Download CSV template"
        onClick={() =>
          downloadBlob(new Blob([reportingTemplate(register)], { type: "text/csv" }), `${register}-template.csv`)
        }
      />
      {!progress ? (
        <>
          <p>Use the template columns or a JSON array. Times use ISO 8601 with an offset. Decimals stay quoted text.</p>
          {register === "moduleCoverage" ? (
            <p>
              Separate owned board coverage IDs with semicolons in CSV. Supporting evidence in other registers must
              already exist.
            </p>
          ) : null}
          <label className="flex flex-col gap-2">
            Facts file (CSV or JSON)
            <input
              type="file"
              accept=".csv,.json,text/csv,application/json"
              onChange={async (event) => {
                const selected = ++version.current;
                const file = event.target.files?.[0];
                setFacts([]);
                setError("");
                if (!file) return;
                try {
                  if (file.size > MAX_REPORTING_IMPORT_BYTES) throw new Error("Choose a file smaller than 5 MB.");
                  const text = await file.text();
                  if (signal.aborted || lifetime.current?.signal.aborted || selected !== version.current) return;
                  setFacts(
                    importReportingRows(register, text, file.name.toLowerCase().endsWith(".json") ? "json" : "csv"),
                  );
                } catch (cause) {
                  if (!signal.aborted && !lifetime.current?.signal.aborted && selected === version.current)
                    setError(getErrorMessage(cause));
                }
              }}
            />
          </label>
          {facts.length ? (
            <>
              <p>{facts.length} rows ready to add or update.</p>
              <ReportingRows rows={facts}>{(page) => <FactDetails facts={page} />}</ReportingRows>
            </>
          ) : null}
          <Input id="import-evidence" label="Import evidence reference" initValue={sourceRef} onChange={setSourceRef} />
        </>
      ) : (
        <>
          <p role="status">
            {progress.confirmedRows} rows confirmed committed · Revision {progress.revision.toString()}.
          </p>
          {progress.status === "unknown" ? (
            <p>
              The last submitted batch has an unknown outcome. Refresh inputs and review the submitted rows before any
              deliberate retry.
            </p>
          ) : progress.status === "complete" ? (
            <p>Import complete.</p>
          ) : progress.status === "cancelled" ? (
            <p>Import stopped. Confirmed batches remain saved.</p>
          ) : null}
          {progress.error ? <ReportingError error={progress.error} /> : null}
        </>
      )}
      {error ? <ReportingError error={error} /> : null}
    </Modal>
  );
};

const RegisterPage = ({ register, setup, signal, onChanged }: { register: ReportingRegister } & Props) => {
  const revision = setup.input?.revision ?? 0n;
  const [pages, setPages] = useState([{ cursor: "", offset: 0 }]);
  const [pageRevision, setPageRevision] = useState(revision);
  if (pageRevision !== revision) {
    setPageRevision(revision);
    setPages([{ cursor: "", offset: 0 }]);
  }
  const current = pages[pages.length - 1];
  const [editing, setEditing] = useState<{ fact?: ReportingFact; deleting?: boolean; revision: bigint } | null>(null);
  const [importing, setImporting] = useState<bigint | null>(null);
  const [retained, setRetained] = useState(() => readRetainedImport(setup.accountId));
  const load = useCallback(
    (requestSignal: AbortSignal) =>
      hostedAccountsClient.listHostReportingFacts(
        {
          accountId: setup.accountId,
          kind: reportingKinds[register],
          expectedRevision: revision,
          cursor: current.cursor,
          pageSize: 50,
        },
        { signal: AbortSignal.any([signal, requestSignal]), timeoutMs: ACCOUNT_READ_TIMEOUT_MS },
      ),
    [setup.accountId, register, revision, current.cursor, signal],
  );
  const query = useAccountQuery(load, revision > 0n);
  const facts = query.data?.facts ?? [];
  const count = setup.input?.registerCounts.find((value) => value.kind === reportingKinds[register])?.count ?? 0n;
  return (
    <>
      <div className="flex flex-wrap gap-3">
        <Button variant="secondary" size={sizes.compact} text="Add fact" onClick={() => setEditing({ revision })} />
        <Button variant="secondary" size={sizes.compact} text="Import facts" onClick={() => setImporting(revision)} />
      </div>
      {retained ? (
        <div className="flex flex-col gap-3">
          <p>
            {retained.progress.confirmedRows} imported rows confirmed committed · Revision{" "}
            {retained.progress.revision.toString()}.
          </p>
          {retained.progress.status === "unknown" ? (
            <>
              <p>The last submitted batch has an unknown outcome. Refresh inputs and review it before retrying.</p>
              <Button
                variant="secondary"
                text="Download submitted rows for review"
                onClick={() =>
                  downloadBlob(
                    new Blob([JSON.stringify(retained.progress.pending.map(reportingFactJSON))], {
                      type: "application/json",
                    }),
                    `${retained.register}-submitted.json`,
                  )
                }
              />
            </>
          ) : null}
          <div className="flex flex-wrap gap-3">
            <Button variant="secondary" text="Refresh inputs" onClick={onChanged} />
            <Button
              variant="secondary"
              text="Dismiss import status"
              onClick={() => {
                forgetRetainedImport(setup.accountId);
                setRetained(null);
              }}
            />
          </div>
        </div>
      ) : null}
      <AccountState
        loading={query.loading}
        error={query.error}
        retry={
          query.errorCode === Code.Aborted || query.errorCode === Code.FailedPrecondition ? onChanged : query.refresh
        }
      />
      {query.data ? (
        <>
          {!facts.length ? (
            <p>No fact rows recorded.</p>
          ) : (
            <FactDetails
              facts={facts}
              actions={(fact) => (
                <div className="mt-3 flex flex-wrap gap-3">
                  <Button
                    variant="secondary"
                    size={sizes.compact}
                    text="Edit"
                    ariaLabel={`Edit fact ${reportingFactID(fact)}`}
                    onClick={() => setEditing({ fact, revision: query.data!.revision })}
                  />
                  <Button
                    variant="secondary"
                    size={sizes.compact}
                    text="Delete"
                    ariaLabel={`Delete fact ${reportingFactID(fact)}`}
                    onClick={() => setEditing({ fact, revision: query.data!.revision, deleting: true })}
                  />
                </div>
              )}
            />
          )}
          <ListPagination
            firstItemIndex={current.offset + 1}
            lastItemIndex={current.offset + facts.length}
            total={Number(count)}
            itemName="rows"
            hasPreviousPage={pages.length > 1}
            hasNextPage={!!query.data.nextCursor}
            loading={query.loading}
            onPrevious={() => setPages(pages.slice(0, -1))}
            onNext={() => {
              if (query.data?.nextCursor)
                setPages([...pages, { cursor: query.data.nextCursor, offset: current.offset + facts.length }]);
            }}
          />
        </>
      ) : null}
      {editing ? (
        <FactEditor {...{ register, setup, signal, onChanged }} {...editing} onDismiss={() => setEditing(null)} />
      ) : null}
      {importing !== null ? (
        <ImportFacts
          {...{ register, setup, signal, onChanged }}
          revision={importing}
          onDismiss={() => setImporting(null)}
          onProgress={setRetained}
        />
      ) : null}
    </>
  );
};

export const ReportingRegisters = (props: Props) => {
  const [register, setRegister] = useState<ReportingRegister>("coverage");
  return (
    <section aria-label="Reporting facts" className="flex flex-col gap-4">
      <Select
        id="fact-register"
        label="Fact register"
        value={register}
        options={reportingRegisters.map((value) => ({ value, label: reportLabel(value) }))}
        onChange={(value) => setRegister(value as ReportingRegister)}
      />
      <RegisterPage key={`${props.setup.accountId}:${register}`} {...props} register={register} />
    </section>
  );
};

useFleetStore.subscribe((state, previous) => {
  if (state.auth.sessionGeneration === previous.auth.sessionGeneration) return;
  retainedImports.clear();
  try {
    for (const key of Object.keys(sessionStorage)) if (key.startsWith(recoveryPrefix)) sessionStorage.removeItem(key);
  } catch {
    /* Session storage may be unavailable; the new session uses a different key. */
  }
});
