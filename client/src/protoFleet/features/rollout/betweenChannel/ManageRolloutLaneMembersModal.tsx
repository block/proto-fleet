import { useEffect, useMemo, useRef, useState } from "react";

import { toError } from "@/protoFleet/api/requestErrors";
import type {
  ListRolloutLaneMembersOptions,
  PreviewRolloutLaneMembershipChangeInput,
  PreviewRolloutLaneModelMembershipChangeInput,
  UpdateRolloutLaneMembershipInput,
  UpdateRolloutLaneModelMembershipInput,
} from "@/protoFleet/api/useRolloutApi";
import { rolloutLaneMembershipBlockedReason } from "@/protoFleet/features/rollout/betweenChannel/betweenChannelUtils";
import { firmwareTransitionDisplay } from "@/protoFleet/features/rollout/firmwareTransitionDisplay";
import type {
  RolloutLane,
  RolloutLaneMembershipChangePreview,
  RolloutLaneMembershipMember,
  RolloutLaneMembershipPage,
  RolloutLaneMembershipUpdateResult,
  RolloutRecord,
} from "@/protoFleet/features/rollout/rolloutTypes";
import MinerSelectionModal, {
  type MinerSelectionValue,
} from "@/protoFleet/features/settings/components/Schedules/MinerSelectionModal";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import StatusCircle from "@/shared/components/StatusCircle";

const MEMBERS_PAGE_SIZE = 1000;
const INITIAL_RENDERED_MEMBERS = 100;

interface MembershipConfirmation {
  addDeviceIdentifiers: string[];
  removeDeviceIdentifiers: string[];
  preview: RolloutLaneMembershipChangePreview;
  idempotencyKey: string;
}

interface CommonManageRolloutLaneMembersModalProps {
  open: boolean;
  lane: RolloutLane;
  latestRollout?: RolloutRecord;
  canManage: boolean;
  isSubmitting: boolean;
  error?: string | null;
  onDismiss: () => void;
  onListMembers: (options: ListRolloutLaneMembersOptions) => Promise<RolloutLaneMembershipPage>;
  onUpdated: (result: RolloutLaneMembershipUpdateResult) => void;
}

interface LegacyManageRolloutLaneMembersModalProps extends CommonManageRolloutLaneMembersModalProps {
  mode: "legacy";
  onPreview: (input: PreviewRolloutLaneMembershipChangeInput) => Promise<RolloutLaneMembershipChangePreview>;
  onUpdate: (input: UpdateRolloutLaneMembershipInput) => Promise<RolloutLaneMembershipUpdateResult>;
  onPreviewModel?: never;
  onUpdateModel?: never;
}

interface ModelManageRolloutLaneMembersModalProps extends CommonManageRolloutLaneMembersModalProps {
  mode: "model";
  onPreview?: never;
  onUpdate?: never;
  onPreviewModel: (input: PreviewRolloutLaneModelMembershipChangeInput) => Promise<RolloutLaneMembershipChangePreview>;
  onUpdateModel: (input: UpdateRolloutLaneModelMembershipInput) => Promise<RolloutLaneMembershipUpdateResult>;
}

type ManageRolloutLaneMembersModalProps =
  LegacyManageRolloutLaneMembersModalProps | ModelManageRolloutLaneMembersModalProps;

interface CachedMembershipPage {
  members: RolloutLaneMembershipMember[];
  totalCount: number;
}

function countLabel(count: number): string {
  return `${count.toLocaleString()} ${count === 1 ? "miner" : "miners"}`;
}

function stableDiffHash(additions: string[], removals: string[]): string {
  let hash = 2166136261;
  const value = `add:${additions.join("\0")}\0remove:${removals.join("\0")}`;
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return (hash >>> 0).toString(36);
}

function membershipIdempotencyKey(laneId: string, revision: bigint, additions: string[], removals: string[]): string {
  return `update-rollout-lane-membership:${laneId}:${revision}:${stableDiffHash(additions, removals)}`;
}

function releaseStatus(member: RolloutLaneMembershipMember): string {
  return member.onCurrentChannel ? "Current release" : "Historical release";
}

function MemberTable({ laneLabel, members }: { laneLabel: string; members: RolloutLaneMembershipMember[] }) {
  const [visibleCount, setVisibleCount] = useState(INITIAL_RENDERED_MEMBERS);
  if (members.length === 0) {
    return (
      <div className="rounded-xl border border-border-5 bg-surface-base px-4 py-10 text-center">
        <div className="text-emphasis-300 text-text-primary">No miners in this lane</div>
        <div className="mt-1 text-300 text-text-primary-70">Change miners to add this lane's first member.</div>
      </div>
    );
  }
  const visibleMembers = members.slice(0, visibleCount);

  return (
    <div>
      <div className="overflow-x-auto rounded-xl border border-border-5 bg-surface-base">
        <table aria-label={`${laneLabel} lane miners`} className="w-full min-w-[1120px] text-left text-300">
          <caption className="sr-only">Miners assigned to {laneLabel}</caption>
          <thead className="border-b border-border-5 text-200 text-text-primary-50">
            <tr>
              <th scope="col" className="px-4 py-3 font-normal">
                Miner
              </th>
              <th scope="col" className="px-4 py-3 font-normal">
                Model
              </th>
              <th scope="col" className="px-4 py-3 font-normal">
                Observed firmware
              </th>
              <th scope="col" className="px-4 py-3 font-normal">
                Pinned lane release
              </th>
              <th scope="col" className="px-4 py-3 font-normal">
                Release status
              </th>
              <th scope="col" className="px-4 py-3 font-normal">
                Enforcement
              </th>
              <th scope="col" className="px-4 py-3 font-normal">
                Details
              </th>
            </tr>
          </thead>
          <tbody>
            {visibleMembers.map((member) => {
              const enforcementDisplay = member.enforcement
                ? firmwareTransitionDisplay[member.enforcement.state]
                : undefined;
              return (
                <tr key={member.deviceIdentifier} className="border-b border-border-5 last:border-b-0">
                  <td className="px-4 py-3 text-emphasis-300 text-text-primary">{member.deviceIdentifier}</td>
                  <td className="px-4 py-3 text-text-primary">
                    {[member.manufacturer, member.model].filter(Boolean).join(" ") || "Unknown"}
                  </td>
                  <td className="px-4 py-3 text-text-primary">{member.observedFirmwareVersion?.trim() || "Unknown"}</td>
                  <td className="px-4 py-3 text-text-primary">{member.pinnedReleaseVersion || "Unknown"}</td>
                  <td className="px-4 py-3 text-text-primary">{releaseStatus(member)}</td>
                  <td className="px-4 py-3 text-text-primary">
                    {enforcementDisplay ? (
                      <span className="flex items-center gap-2 whitespace-nowrap">
                        <StatusCircle status={enforcementDisplay.status} variant="simple" width="w-[6px]" />
                        {enforcementDisplay.tableLabel}
                      </span>
                    ) : (
                      "—"
                    )}
                  </td>
                  <td className="max-w-80 px-4 py-3 text-text-primary-70">{member.enforcement?.lastError ?? "—"}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      {visibleCount < members.length ? (
        <div className="mt-3 flex justify-center">
          <Button
            text="Show more"
            ariaLabel="Show more miners"
            variant={variants.secondary}
            size={sizes.compact}
            onClick={() => setVisibleCount((current) => Math.min(current + INITIAL_RENDERED_MEMBERS, members.length))}
          />
        </div>
      ) : null}
    </div>
  );
}

export default function ManageRolloutLaneMembersModal(props: ManageRolloutLaneMembersModalProps) {
  const { open, lane, latestRollout, canManage, isSubmitting, error, onDismiss, onListMembers, onUpdated } = props;
  const [members, setMembers] = useState<RolloutLaneMembershipMember[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [isLoadingAllMembers, setIsLoadingAllMembers] = useState(false);
  const [hasLoadedAllMembers, setHasLoadedAllMembers] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [loadAttempt, setLoadAttempt] = useState(0);
  const [actionError, setActionError] = useState<string | null>(null);
  const [showMinerSelection, setShowMinerSelection] = useState(false);
  const [isPreviewing, setIsPreviewing] = useState(false);
  const [confirmation, setConfirmation] = useState<MembershipConfirmation | null>(null);
  const [selectedModelId, setSelectedModelId] = useState<string | null>(lane.models[0]?.id ?? null);
  const membershipCacheRef = useRef(new Map<string, CachedMembershipPage>());
  const selectedModel =
    props.mode === "model" ? (lane.models.find((model) => model.id === selectedModelId) ?? lane.models[0]) : undefined;
  const membershipScopeKey =
    props.mode === "model"
      ? selectedModel
        ? `${selectedModel.id}:${selectedModel.revision}`
        : null
      : `${lane.id}:${lane.revision}`;
  const membershipIsLoading = membershipScopeKey !== null && isLoading;
  const displayedMembers = useMemo(
    () => (selectedModel || props.mode === "legacy" ? members : []),
    [members, props.mode, selectedModel],
  );
  const currentMemberIdentifiers = useMemo(
    () => displayedMembers.map((member) => member.deviceIdentifier),
    [displayedMembers],
  );
  const blockedReason = lane.topologyEnabled ? null : rolloutLaneMembershipBlockedReason(lane, latestRollout);

  useEffect(() => {
    if (!open) {
      return;
    }
    if (!membershipScopeKey) {
      return;
    }
    const controller = new AbortController();
    const cached = membershipCacheRef.current.get(membershipScopeKey);
    // eslint-disable-next-line react-hooks/set-state-in-effect -- resets modal state before loading an external API
    setMembers(cached?.members ?? []);
    setTotalCount(cached?.totalCount ?? 0);
    setIsLoading(!cached);
    setIsLoadingAllMembers(false);
    setHasLoadedAllMembers(Boolean(cached));
    setLoadError(null);
    setActionError(null);
    setShowMinerSelection(false);
    setConfirmation(null);
    if (cached) {
      return () => controller.abort();
    }

    void (async () => {
      const seenPageTokens = new Set<string>();
      try {
        const firstPage = await onListMembers({
          laneId: lane.id,
          laneModelId: selectedModel?.id,
          pageSize: MEMBERS_PAGE_SIZE,
          pageToken: "",
          includeTotalCount: true,
          signal: controller.signal,
        });
        if (controller.signal.aborted) {
          return;
        }
        const loadedMembers = [...firstPage.members];
        setMembers(loadedMembers);
        setTotalCount(firstPage.totalCount);
        setIsLoading(false);

        let pageToken = firstPage.nextPageToken;
        setIsLoadingAllMembers(pageToken !== "");
        while (pageToken) {
          if (seenPageTokens.has(pageToken)) {
            throw new Error("Lane membership pagination repeated a page.");
          }
          seenPageTokens.add(pageToken);
          const page = await onListMembers({
            laneId: lane.id,
            laneModelId: selectedModel?.id,
            pageSize: MEMBERS_PAGE_SIZE,
            pageToken,
            includeTotalCount: false,
            signal: controller.signal,
          });
          if (controller.signal.aborted) {
            return;
          }
          loadedMembers.push(...page.members);
          setMembers([...loadedMembers]);
          pageToken = page.nextPageToken;
        }
        membershipCacheRef.current.set(membershipScopeKey, {
          members: [...loadedMembers],
          totalCount: firstPage.totalCount,
        });
        setHasLoadedAllMembers(true);
      } catch (loadFailure) {
        if (!controller.signal.aborted) {
          setLoadError(toError(loadFailure, "Couldn't load lane members. Try again.").message);
        }
      } finally {
        if (!controller.signal.aborted) {
          setIsLoading(false);
          setIsLoadingAllMembers(false);
        }
      }
    })();

    return () => controller.abort();
  }, [lane.id, loadAttempt, membershipScopeKey, onListMembers, open, selectedModel?.id]);

  const handleMinerSelection = async (selection: MinerSelectionValue) => {
    const current = new Set(currentMemberIdentifiers);
    const selected = new Set(selection.selectedMinerIds);
    const addDeviceIdentifiers = [...selected].filter((identifier) => !current.has(identifier)).sort();
    const removeDeviceIdentifiers = [...current].filter((identifier) => !selected.has(identifier)).sort();
    if (addDeviceIdentifiers.length === 0 && removeDeviceIdentifiers.length === 0) {
      setShowMinerSelection(false);
      return;
    }

    setShowMinerSelection(false);
    setActionError(null);
    setIsPreviewing(true);
    try {
      let preview: RolloutLaneMembershipChangePreview;
      if (props.mode === "model") {
        if (!selectedModel) {
          throw new Error("A model declaration is required to change model membership.");
        }
        preview = await props.onPreviewModel({
          laneId: lane.id,
          laneModelId: selectedModel.id,
          addDeviceIdentifiers,
          removeDeviceIdentifiers,
        });
      } else {
        preview = await props.onPreview({
          laneId: lane.id,
          addDeviceIdentifiers,
          removeDeviceIdentifiers,
        });
      }
      setConfirmation({
        addDeviceIdentifiers,
        removeDeviceIdentifiers,
        preview,
        idempotencyKey: membershipIdempotencyKey(
          `${lane.id}:${selectedModel?.id ?? "legacy"}`,
          selectedModel?.revision ?? lane.revision,
          addDeviceIdentifiers,
          removeDeviceIdentifiers,
        ),
      });
    } catch (previewFailure) {
      setActionError(toError(previewFailure, "Couldn't preview membership changes. Try again.").message);
    } finally {
      setIsPreviewing(false);
    }
  };

  const confirmMembershipChange = async () => {
    if (!confirmation) {
      return;
    }
    try {
      const input = {
        laneId: lane.id,
        expectedRevision: selectedModel?.revision ?? lane.revision,
        addDeviceIdentifiers: confirmation.addDeviceIdentifiers,
        removeDeviceIdentifiers: confirmation.removeDeviceIdentifiers,
        confirmFirmware: confirmation.preview.requiresFirmwareConfirmation,
        confirmReassign: confirmation.preview.requiresReassignmentConfirmation,
        idempotencyKey: confirmation.idempotencyKey,
        reason: selectedModel ? "Update rollout lane model membership" : "Update rollout lane membership",
      };
      let result: RolloutLaneMembershipUpdateResult;
      if (props.mode === "model") {
        if (!selectedModel) {
          throw new Error("A model declaration is required to change model membership.");
        }
        result = await props.onUpdateModel({ ...input, laneModelId: selectedModel.id });
      } else {
        result = await props.onUpdate(input);
      }
      membershipCacheRef.current.clear();
      onUpdated(result);
      if (result.transitionMembers.length === 0) {
        setConfirmation(null);
      }
    } catch {
      // The API hook owns mutationError so retries share one authoritative error state.
    }
  };

  const reassignmentSourceLabels = confirmation
    ? [...new Set(confirmation.preview.reassignments.map((reassignment) => reassignment.sourceLaneLabel))].join(", ")
    : "";
  const reassignmentCount = confirmation?.preview.reassignments.length ?? 0;
  const additionCount = Math.max(0, (confirmation?.addDeviceIdentifiers.length ?? 0) - reassignmentCount);
  const removalCount = confirmation?.removeDeviceIdentifiers.length ?? 0;
  const firmwareUpdateCount = confirmation
    ? confirmation.preview.targetFirmwarePreview.mismatchedCount +
      confirmation.preview.targetFirmwarePreview.unknownCount
    : 0;
  const confirmationIntent = confirmation?.preview.requiresReassignmentConfirmation
    ? "critical"
    : confirmation?.preview.requiresFirmwareConfirmation
      ? "warning"
      : "info";

  return (
    <>
      <Modal
        open={open}
        title={`Manage ${lane.label} miners`}
        size="large"
        onDismiss={onDismiss}
        className="flex !h-[calc(100dvh-(--spacing(32)))] max-h-[calc(100dvh-(--spacing(32)))] flex-col !overflow-hidden"
        bodyClassName="flex min-h-0 flex-1 flex-col"
      >
        <div className="flex min-h-0 flex-1 flex-col gap-5">
          {props.mode === "model" && lane.models.length ? (
            <div className="flex flex-wrap gap-2" role="group" aria-label="Model declarations">
              {lane.models.map((model) => (
                <Button
                  key={model.id}
                  text={`${model.manufacturer} ${model.model} (${countLabel(model.memberCount)})`}
                  variant={model.id === selectedModelId ? variants.primary : variants.secondary}
                  size={sizes.compact}
                  ariaPressed={model.id === selectedModelId}
                  disabled={isSubmitting}
                  onClick={() => {
                    setSelectedModelId(model.id);
                    setConfirmation(null);
                    setActionError(null);
                  }}
                />
              ))}
            </div>
          ) : null}
          <div className="flex items-start justify-between gap-4 phone:flex-col phone:items-stretch">
            <div>
              <div className="text-300 text-text-primary-70">
                {membershipIsLoading ? "Loading members" : countLabel(totalCount)}
              </div>
              {isLoadingAllMembers && canManage ? (
                <div className="mt-1 max-w-2xl text-200 text-text-primary-70" aria-live="polite">
                  Loading all members…
                </div>
              ) : blockedReason && canManage ? (
                <div className="mt-1 max-w-2xl text-200 text-text-primary-70">{blockedReason}</div>
              ) : null}
            </div>
            {canManage ? (
              <Button
                text={isPreviewing ? "Preparing review..." : "Change miners"}
                variant={variants.secondary}
                size={sizes.compact}
                disabled={!hasLoadedAllMembers || isPreviewing || blockedReason !== null}
                onClick={() => setShowMinerSelection(true)}
              />
            ) : null}
          </div>

          {loadError ? (
            <Callout
              intent={intents.danger}
              prefixIcon={<Alert />}
              title="Lane members are unavailable"
              subtitle={loadError}
              buttonText="Retry"
              buttonOnClick={() => setLoadAttempt((current) => current + 1)}
            />
          ) : null}
          {actionError ? (
            <Callout
              intent={intents.danger}
              prefixIcon={<Alert />}
              title="Membership changes could not be previewed"
              subtitle={actionError}
            />
          ) : null}
          {error ? (
            <Callout
              intent={intents.danger}
              prefixIcon={<Alert />}
              title="Membership could not be updated"
              subtitle={error}
            />
          ) : null}

          <div className="min-h-0 flex-1 overflow-auto">
            {membershipIsLoading ? (
              <div
                aria-busy="true"
                aria-live="polite"
                className="flex items-center justify-center gap-3 py-12 text-300 text-text-primary-70"
              >
                <ProgressCircular indeterminate className="text-core-primary-fill" />
                Loading lane members...
              </div>
            ) : (
              <MemberTable
                laneLabel={
                  selectedModel ? `${lane.label} ${selectedModel.manufacturer} ${selectedModel.model}` : lane.label
                }
                members={displayedMembers}
              />
            )}
          </div>
        </div>
      </Modal>

      <MinerSelectionModal
        open={showMinerSelection}
        selectedMinerIds={currentMemberIdentifiers}
        showSelectAllFooter={false}
        showRolloutLaneColumn
        onDismiss={() => setShowMinerSelection(false)}
        onSave={(selection) => void handleMinerSelection(selection)}
      />

      <Dialog
        open={confirmation !== null}
        title="Review membership changes"
        testId="rollout-lane-membership-confirmation"
        onDismiss={() => {
          if (!isSubmitting) {
            setConfirmation(null);
          }
        }}
        icon={
          <DialogIcon intent={confirmationIntent}>
            <Alert />
          </DialogIcon>
        }
        buttons={[
          {
            text: "Cancel",
            variant: variants.secondary,
            disabled: isSubmitting,
            onClick: () => setConfirmation(null),
          },
          {
            text: "Confirm membership changes",
            variant: variants.primary,
            loading: isSubmitting,
            onClick: () => void confirmMembershipChange(),
          },
        ]}
      >
        {confirmation ? (
          <div className="grid gap-3 text-300 text-text-primary-70">
            {additionCount > 0 ? <p>Additions: {countLabel(additionCount)}</p> : null}
            {removalCount > 0 ? (
              <p>
                Removals: {countLabel(removalCount)}. Firmware will not change, and Fleet will stop managing{" "}
                {removalCount === 1 ? "it" : "them"} through this lane.
              </p>
            ) : null}
            {reassignmentCount > 0 ? (
              <p>
                Reassignments: {countLabel(reassignmentCount)}
                {reassignmentSourceLabels ? ` from ${reassignmentSourceLabels}` : ""}.
              </p>
            ) : null}
            {firmwareUpdateCount > 0 ? (
              <p>
                {countLabel(firmwareUpdateCount)} {firmwareUpdateCount === 1 ? "has" : "have"} mismatched or unknown
                target firmware. Updates begin after confirmation.
              </p>
            ) : null}
          </div>
        ) : null}
      </Dialog>
    </>
  );
}
