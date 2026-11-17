import { useCallback, useContext, useEffect, useMemo, useState } from "react";
import { clone, create } from "@bufbuild/protobuf";

import { ACCOUNT_READ_TIMEOUT_MS, createAccountConnection, hostedAccountsClient } from "../api";
import { reportLabel, reportPrerequisites, reportTime } from "../reportFormat";
import { useAccountQuery } from "../useAccountQuery";
import { AccountState } from "./AccountState";
import { ReportingError } from "./ReportingError";
import { ReportingCheck, ReportingDate, ReportingForm } from "./ReportingForm";
import { ReportingRegisters } from "./ReportingRegisters";
import { authClient } from "@/protoFleet/api/clients";
import {
  type HostReportingSetup,
  type ReportingConfiguration,
  ReportingConfigurationSchema,
  ReportingPrerequisiteSchema,
  ReportingRegisterCompletenessSchema,
  ReportingRepresentativeSchema,
  ReportingScheduleSchema,
  ReportingSourceSchema,
} from "@/protoFleet/api/generated/hosted/v1/reporting_pb";
import { SitesContext } from "@/protoFleet/api/SitesContext";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { usePermissions } from "@/protoFleet/store";
import Button, { sizes } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";
import Textarea from "@/shared/components/Textarea";

type UserChoice = { userId: string; username: string };
const prerequisiteLabels = Object.fromEntries(
  Object.entries(reportPrerequisites).map(([key, value]) => [key, value.label]),
);

const ConfigurationFields = ({
  configuration,
  onChange,
  users,
  owners,
  canEditRepresentatives = false,
}: {
  configuration: ReportingConfiguration;
  onChange: (configuration: ReportingConfiguration) => void;
  users: UserChoice[];
  owners: UserChoice[];
  canEditRepresentatives?: boolean;
}) => {
  const update = (mutate: (next: ReportingConfiguration) => void) => {
    const next = clone(ReportingConfigurationSchema, configuration);
    mutate(next);
    onChange(next);
  };
  const userOptions = users.map((user) => ({ value: user.userId, label: user.username }));
  return (
    <>
      <Input
        id="report-contract"
        label="Contract evidence reference"
        initValue={configuration.contractRef}
        onChange={(value) =>
          update((next) => {
            next.contractRef = value;
          })
        }
      />
      <Input
        id="report-timezone"
        label="Reporting timezone"
        initValue={configuration.timezone}
        onChange={(value) =>
          update((next) => {
            next.timezone = value;
          })
        }
      />
      <div className="grid grid-cols-2 gap-3">
        <Input
          id="report-year-month"
          label="Reporting year start month"
          type="number"
          initValue={configuration.reportingYearMonth}
          onChange={(value) =>
            update((next) => {
              next.reportingYearMonth = Number(value);
            })
          }
        />
        <Input
          id="report-year-day"
          label="Reporting year start day"
          type="number"
          initValue={configuration.reportingYearDay}
          onChange={(value) =>
            update((next) => {
              next.reportingYearDay = Number(value);
            })
          }
        />
      </div>
      <Textarea
        id="report-holidays"
        label="Business calendar holidays (YYYY-MM-DD, one per line)"
        initValue={configuration.holidays.join("\n")}
        onChange={(value) =>
          update((next) => {
            next.holidays = value
              .split(/\n/)
              .map((day) => day.trim())
              .filter(Boolean);
          })
        }
      />
      <Input
        id="report-delivery-channel"
        label="Agreed delivery channel"
        initValue={configuration.deliveryChannel}
        onChange={(value) =>
          update((next) => {
            next.deliveryChannel = value;
          })
        }
      />
      <Input
        id="report-shared-allocation"
        label="Shared-container allocation evidence"
        initValue={configuration.sharedAllocationRef}
        onChange={(value) =>
          update((next) => {
            next.sharedAllocationRef = value;
          })
        }
      />
      <details>
        <summary className="cursor-pointer text-heading-100">Prerequisites</summary>
        <div className="mt-3 flex flex-col gap-4">
          {["G1", "G2", "G3", "G4", "G5"].map((key) => {
            const item =
              configuration.prerequisites.find((item) => item.key === key) ??
              create(ReportingPrerequisiteSchema, { key });
            const change = (patch: Partial<typeof item>) =>
              update((next) => {
                next.prerequisites = [...next.prerequisites.filter((item) => item.key !== key), { ...item, ...patch }];
              });
            return (
              <div key={key} className="flex flex-col gap-3">
                <ReportingCheck
                  label={prerequisiteLabels[key]}
                  checked={item.ready}
                  onChange={(ready) => change({ ready })}
                />
                <Input
                  id={`prereq-${key}-owner`}
                  label="Accountable owner"
                  initValue={item.owner}
                  onChange={(owner) => change({ owner })}
                />
                <Input
                  id={`prereq-${key}-source`}
                  label="Approval evidence reference"
                  initValue={item.sourceRef}
                  onChange={(sourceRef) => change({ sourceRef })}
                />
              </div>
            );
          })}
        </div>
      </details>
      <details>
        <summary className="cursor-pointer text-heading-100">Source certification</summary>
        <div className="mt-3 flex flex-col gap-4">
          {configuration.sources.map((item, index) => (
            <div key={index} className="flex flex-col gap-3">
              <Input
                id={`source-${index}-id`}
                label="Source name"
                initValue={item.id}
                onChange={(value) =>
                  update((next) => {
                    next.sources[index].id = value;
                  })
                }
              />
              <Select
                id={`source-${index}-metric`}
                label="Measure"
                value={item.metric}
                options={["meter", "pool", "hashrate", "host"].map((value) => ({ value, label: reportLabel(value) }))}
                onChange={(value) =>
                  update((next) => {
                    next.sources[index].metric = value;
                  })
                }
              />
              <ReportingCheck
                label="Certified"
                checked={item.certified}
                onChange={(value) =>
                  update((next) => {
                    next.sources[index].certified = value;
                  })
                }
              />
              <Input
                id={`source-${index}-ref`}
                label="Certification evidence reference"
                initValue={item.sourceRef}
                onChange={(value) =>
                  update((next) => {
                    next.sources[index].sourceRef = value;
                  })
                }
              />
              <Button
                variant="textOnly"
                text="Remove source"
                onClick={() =>
                  update((next) => {
                    next.sources.splice(index, 1);
                  })
                }
              />
            </div>
          ))}
          <Button
            variant="secondary"
            text="Add source"
            onClick={() =>
              update((next) => {
                next.sources.push(create(ReportingSourceSchema, { metric: "host" }));
              })
            }
          />
        </div>
      </details>
      <details>
        <summary className="cursor-pointer text-heading-100">Authorized representatives</summary>
        {!canEditRepresentatives ? (
          <p className="mt-3 text-200 text-text-primary-70">
            Team management access is required to change representatives.
          </p>
        ) : null}
        <fieldset disabled={!canEditRepresentatives} className="mt-3 flex flex-col gap-4">
          {configuration.representatives.map((item, index) => (
            <div key={index} className="flex flex-col gap-3">
              <Select
                id={`representative-${index}`}
                label="Representative"
                value={item.userId}
                options={userOptions}
                placeholder="Existing representative"
                disabled={!canEditRepresentatives}
                onChange={(value) =>
                  update((next) => {
                    next.representatives[index].userId = value;
                  })
                }
              />
              <ReportingCheck
                label="Contractual submission authority"
                checked={item.canSubmit}
                onChange={(value) =>
                  update((next) => {
                    next.representatives[index].canSubmit = value;
                  })
                }
              />
              <Input
                id={`representative-${index}-ref`}
                label="Authority evidence reference"
                initValue={item.sourceRef}
                onChange={(value) =>
                  update((next) => {
                    next.representatives[index].sourceRef = value;
                  })
                }
              />
              <Button
                variant="textOnly"
                text="Remove representative"
                onClick={() =>
                  update((next) => {
                    next.representatives.splice(index, 1);
                  })
                }
              />
            </div>
          ))}
          <Button
            variant="secondary"
            text="Add representative"
            disabled={!users.length}
            onClick={() =>
              update((next) => {
                next.representatives.push(create(ReportingRepresentativeSchema, { userId: users[0].userId }));
              })
            }
          />
        </fieldset>
      </details>
      <details>
        <summary className="cursor-pointer text-heading-100">Register completeness</summary>
        <div className="mt-3 flex flex-col gap-4">
          {configuration.registers.map((item, index) => (
            <div key={index} className="flex flex-col gap-3">
              <Select
                id={`register-${index}`}
                label="Register"
                value={item.kind}
                options={["incidents", "obligations", "fees", "pool", "hashrate", "coverage"].map((value) => ({
                  value,
                  label: reportLabel(value),
                }))}
                onChange={(value) =>
                  update((next) => {
                    next.registers[index].kind = value;
                  })
                }
              />
              <ReportingDate
                id={`register-${index}-start`}
                label="Complete from"
                value={item.start}
                onChange={(value) =>
                  update((next) => {
                    next.registers[index].start = value;
                  })
                }
              />
              <ReportingDate
                id={`register-${index}-end`}
                label="Complete until"
                value={item.end}
                onChange={(value) =>
                  update((next) => {
                    next.registers[index].end = value;
                  })
                }
              />
              <Input
                id={`register-${index}-ref`}
                label="Completeness evidence reference"
                initValue={item.sourceRef}
                onChange={(value) =>
                  update((next) => {
                    next.registers[index].sourceRef = value;
                  })
                }
              />
              <Button
                variant="textOnly"
                text="Remove completeness period"
                onClick={() =>
                  update((next) => {
                    next.registers.splice(index, 1);
                  })
                }
              />
            </div>
          ))}
          <Button
            variant="secondary"
            text="Add completeness period"
            onClick={() =>
              update((next) => {
                next.registers.push(create(ReportingRegisterCompletenessSchema, { kind: "coverage" }));
              })
            }
          />
        </div>
      </details>
      <details>
        <summary className="cursor-pointer text-heading-100">Reporting schedule</summary>
        <div className="mt-3 flex flex-col gap-3">
          <ReportingCheck
            label="Schedule recurring reports"
            checked={!!configuration.schedule}
            onChange={(checked) =>
              update((next) => {
                next.schedule = checked ? create(ReportingScheduleSchema) : undefined;
              })
            }
          />
          {configuration.schedule ? (
            <>
              <Select
                id="schedule-owner"
                label="Reporting owner"
                value={configuration.schedule.ownerUserId}
                options={owners.map((owner) => ({ value: owner.userId, label: owner.username }))}
                onChange={(value) =>
                  update((next) => {
                    next.schedule!.ownerUserId = value;
                  })
                }
              />
              <Input
                id="schedule-ref"
                label="Schedule evidence reference"
                initValue={configuration.schedule.sourceRef}
                onChange={(value) =>
                  update((next) => {
                    next.schedule!.sourceRef = value;
                  })
                }
              />
              {(["monthlyStart", "quarterlyStart", "annualStart"] as const).map((field) => (
                <ReportingDate
                  key={field}
                  id={`schedule-${field}`}
                  timezone={configuration.timezone || "UTC"}
                  label={reportLabel(field)}
                  value={configuration.schedule?.[field]}
                  onChange={(value) =>
                    update((next) => {
                      next.schedule![field] = value;
                    })
                  }
                />
              ))}
            </>
          ) : null}
        </div>
      </details>
    </>
  );
};

const ReportingSetup = ({
  setup,
  signal,
  onChanged,
}: {
  setup: HostReportingSetup;
  signal: AbortSignal;
  onChanged: () => void;
}) => {
  const permissions = usePermissions();
  const siteCatalog = useContext(SitesContext);
  const sites = siteCatalog?.sites ?? [];
  const [action, setAction] = useState<"configuration" | "authority" | null>(null);
  const [sourceRef, setSourceRef] = useState("");
  const original =
    setup.input?.configuration ?? create(ReportingConfigurationSchema, { reportingYearMonth: 1, reportingYearDay: 1 });
  const [draft, setDraft] = useState(() => clone(ReportingConfigurationSchema, original));
  const [draftRevision, setDraftRevision] = useState(setup.input?.revision ?? 0n);
  const [draftRepresentatives, setDraftRepresentatives] = useState(
    () => clone(ReportingConfigurationSchema, original).representatives,
  );
  const [userId, setUserId] = useState("");
  const [canCreate, setCanCreate] = useState(false);
  const [allSites, setAllSites] = useState(false);
  const [siteIds, setSiteIds] = useState<bigint[]>([]);
  const loadUsers = useCallback(
    (requestSignal: AbortSignal) => authClient.listUsers({}, { signal: AbortSignal.any([signal, requestSignal]) }),
    [signal],
  );
  const directory = useAccountQuery(loadUsers, setup.canAdminister && permissions.includes("user:read"));
  const users = useMemo(() => {
    const seen = new Set<string>();
    return [...setup.owners, ...setup.authorities, ...(directory.data?.users ?? [])].filter((user) => {
      if (seen.has(user.userId)) return false;
      seen.add(user.userId);
      return true;
    });
  }, [setup.owners, setup.authorities, directory.data?.users]);
  const memberConnection = useMemo(() => createAccountConnection(setup.accountId), [setup.accountId]);
  useEffect(() => {
    memberConnection.activate();
    return () => memberConnection.dispose();
  }, [memberConnection]);
  const loadMembers = useCallback(
    (requestSignal: AbortSignal) =>
      memberConnection.client.listMembers(
        { accountId: setup.accountId },
        { signal: AbortSignal.any([signal, requestSignal]), timeoutMs: ACCOUNT_READ_TIMEOUT_MS },
      ),
    [memberConnection, setup.accountId, signal],
  );
  const membersQuery = useAccountQuery(loadMembers, setup.canManageRepresentatives, true, POLL_INTERVAL_MS);
  const members = membersQuery.data?.members ?? [];
  const canEditRepresentatives = setup.canManageRepresentatives && !!membersQuery.data && !membersQuery.error;
  const config = setup.input?.configuration;
  const configurationValid = Boolean(sourceRef.trim() && draft.timezone.trim());
  const authorityValid = Boolean(userId && (allSites || siteIds.length > 0 || !canCreate));
  const open = (nextAction: typeof action) => {
    setDraft(clone(ReportingConfigurationSchema, original));
    setDraftRevision(setup.input?.revision ?? 0n);
    setDraftRepresentatives(clone(ReportingConfigurationSchema, original).representatives);
    setSourceRef("");
    setAction(nextAction);
  };
  const dismiss = () => setAction(null);
  const save = async (requestSignal: AbortSignal) => {
    const configuration = clone(ReportingConfigurationSchema, draft);
    if (!canEditRepresentatives) configuration.representatives = draftRepresentatives;
    await hostedAccountsClient.mutateHostReportingInputs(
      {
        accountId: setup.accountId,
        expectedRevision: draftRevision,
        sourceRef: sourceRef.trim(),
        configuration,
      },
      { signal: requestSignal },
    );
    if (!requestSignal.aborted) onChanged();
  };
  return (
    <section aria-label="Reporting setup" className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-heading-200">Reporting setup</h2>
        <div className="flex flex-wrap gap-3">
          {setup.canCreate ? (
            <>
              <Button
                variant="secondary"
                size={sizes.compact}
                text="Edit reporting configuration"
                onClick={() => open("configuration")}
              />
            </>
          ) : null}
          {setup.canAdminister ? (
            <Button
              variant="secondary"
              size={sizes.compact}
              text="Reporting permissions"
              onClick={() => open("authority")}
            />
          ) : null}
        </div>
      </div>
      {!setup.input ? <p>No reporting inputs.</p> : null}
      {setup.capture.length ? (
        <details>
          <summary className="cursor-pointer">Preserved source coverage</summary>
          <div className="mt-3 flex flex-col gap-3">
            {setup.capture.map((stream) => (
              <div key={`${stream.sourceId}:${stream.metric}`}>
                <p>
                  {(
                    {
                      1: "Miner measurements",
                      2: "Miner state",
                      3: "Module measurements",
                      4: "Module diagnostics",
                    } as Record<number, string>
                  )[stream.sourceId] || "Unknown source"}{" "}
                  ·{" "}
                  {({ 1: "Hashrate", 2: "Power", 3: "Temperature", 4: "State" } as Record<number, string>)[
                    stream.metric
                  ] || "Unknown measure"}
                </p>
                <p>
                  {stream.expected - stream.missing} of {stream.expected} streams preserved · {stream.missing} missing
                </p>
                <p>
                  Oldest source time: {reportTime(stream.oldestSource)} · Oldest capture:{" "}
                  {reportTime(stream.oldestCapture)}
                </p>
              </div>
            ))}
          </div>
        </details>
      ) : null}
      {config ? (
        <>
          <p>
            {config.timezone || "Timezone unavailable"} · Reporting year starts {config.reportingYearMonth}/
            {config.reportingYearDay}
          </p>
          <p>Evidence: {setup.input?.sourceRef || "Unavailable"}</p>
          {Object.keys(prerequisiteLabels)
            .map(
              (key) =>
                config.prerequisites.find((item) => item.key === key) ?? create(ReportingPrerequisiteSchema, { key }),
            )
            .filter((item) => !item.ready)
            .map((item) => (
              <p key={item.key}>
                {prerequisiteLabels[item.key] ?? item.key}: pending · {item.owner || "Owner required"}
                {item.sourceRef ? ` · ${item.sourceRef}` : ""}
              </p>
            ))}
          <details>
            <summary className="cursor-pointer">Configuration and evidence</summary>
            <dl className="mt-3 flex flex-col gap-2">
              <div>Contract: {config.contractRef || "Unavailable"}</div>
              <div>Holidays: {config.holidays.join(", ") || "None recorded"}</div>
              <div>Delivery: {config.deliveryChannel || "Unavailable"}</div>
              <div>Shared allocation: {config.sharedAllocationRef || "Unavailable"}</div>
              {config.sources.map((item) => (
                <div key={`${item.id}:${item.metric}`}>
                  {item.id} · {reportLabel(item.metric)} · {item.certified ? "Certified" : "Uncertified"} ·{" "}
                  {item.sourceRef}
                </div>
              ))}
              {config.prerequisites.map((item) => (
                <div key={item.key}>
                  {prerequisiteLabels[item.key] ?? item.key} · {item.ready ? "Ready" : "Pending"} ·{" "}
                  {item.owner || "Owner required"} · {item.sourceRef}
                </div>
              ))}
              {config.representatives.map((item) => (
                <div key={item.userId}>
                  {members.find((member) => member.userId === item.userId)?.username || "Authorized representative"} ·{" "}
                  {item.canSubmit ? "Submission" : "No submission"} · {item.sourceRef}
                </div>
              ))}
              {config.registers.map((item, index) => (
                <div key={index}>
                  {reportLabel(item.kind)} complete {reportTime(item.start)} – {reportTime(item.end)} · {item.sourceRef}
                </div>
              ))}
            </dl>
          </details>
          {setup.canCreate ? <ReportingRegisters setup={setup} signal={signal} onChanged={onChanged} /> : null}
        </>
      ) : null}
      {action === "configuration" ? (
        <ReportingForm
          title="Edit reporting configuration"
          confirm="Save inputs"
          signal={signal}
          valid={configurationValid}
          onDismiss={dismiss}
          onConflict={onChanged}
          onSubmit={save}
        >
          <ConfigurationFields
            configuration={draft}
            users={members}
            canEditRepresentatives={canEditRepresentatives}
            owners={setup.owners}
            onChange={setDraft}
          />
          {setup.canManageRepresentatives ? (
            <AccountState loading={membersQuery.loading} error={membersQuery.error} retry={membersQuery.refresh} />
          ) : null}
          <Input
            id="configuration-evidence"
            label="Change evidence reference"
            initValue={sourceRef}
            onChange={setSourceRef}
          />
        </ReportingForm>
      ) : null}
      {action === "authority" ? (
        <ReportingForm
          title="Reporting permissions"
          confirm="Save reporting permissions"
          signal={signal}
          valid={authorityValid}
          onDismiss={dismiss}
          onConflict={onChanged}
          onSubmit={async (requestSignal) => {
            await hostedAccountsClient.setHostReportingAuthority(
              {
                accountId: setup.accountId,
                authority: {
                  userId,
                  canCreate,
                  allSites,
                  siteIds: allSites ? [] : siteIds,
                },
                expectedRevision: setup.authorities.find((item) => item.userId === userId)?.revision ?? 0n,
              },
              { signal: requestSignal },
            );
            if (!requestSignal.aborted) onChanged();
          }}
        >
          <Select
            id="reporting-operator"
            label="Operator"
            value={userId}
            options={users.map((user) => ({ value: user.userId, label: user.username }))}
            onChange={(value) => {
              const authority = setup.authorities.find((item) => item.userId === value);
              setUserId(value);
              setCanCreate(authority?.canCreate ?? false);
              setAllSites(authority?.allSites ?? false);
              setSiteIds(authority?.siteIds ?? []);
            }}
          />
          {directory.error ? <ReportingError error={directory.error} /> : null}
          <ReportingCheck label="Create reports" checked={canCreate} onChange={setCanCreate} />
          <ReportingCheck label="All account sites" checked={allSites} onChange={setAllSites} />
          {!allSites ? (
            !siteCatalog ? (
              <p>Site choices are unavailable.</p>
            ) : !siteCatalog.sitesSettled ? (
              <p role="status">Loading site choices…</p>
            ) : siteCatalog.sitesError ? (
              <div role="alert">
                <p>{siteCatalog.sitesError}</p>
                <Button variant="secondary" text="Retry site choices" onClick={siteCatalog.refetchSites} />
              </div>
            ) : !sites.some(({ site }) => !!site) ? (
              <p>No available site choices.</p>
            ) : null
          ) : null}
          {!allSites
            ? sites.flatMap(({ site }) =>
                site
                  ? [
                      <ReportingCheck
                        key={String(site.id)}
                        label={site.name}
                        checked={siteIds.includes(site.id)}
                        onChange={(checked) =>
                          setSiteIds((previous) =>
                            checked ? [...previous, site.id] : previous.filter((id) => id !== site.id),
                          )
                        }
                      />,
                    ]
                  : [],
              )
            : null}
        </ReportingForm>
      ) : null}
    </section>
  );
};
export default ReportingSetup;
