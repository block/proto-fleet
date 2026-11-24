import { useState } from "react";

import { WorkerNameReview } from "./WorkerNameReview";
import { fleetManagementClient } from "@/protoFleet/api/clients";
import type { HashboardReading } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import {
  type DeviceSelector,
  type ReviewWorkerNamesResponse,
  WorkerNameMode,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import AuthenticateFleetModal from "@/protoFleet/features/auth/components/AuthenticateFleetModal";
import { hasMiningGroupPermissions } from "@/protoFleet/features/containers/miningGroupPermissions";
import { ActionModal } from "@/protoFleet/features/hostedAccounts/components/ActionModal";
import { useFleetStore, usePermissions } from "@/protoFleet/store";
import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";
import { pushToast, STATUSES } from "@/shared/features/toaster";

type Props = {
  deviceSelector?: DeviceSelector;
  moduleIds?: bigint[];
  hashboardIds?: bigint[];
  hashboards?: Pick<HashboardReading, "hashboardId" | "workerName" | "workerNameMode">[];
  onDismiss: () => void;
};

function currentWorkerName({ moduleIds, hashboardIds, hashboards }: Props) {
  if (moduleIds?.length || !hashboardIds?.length) return undefined;
  const selected = hashboardIds.map((id) => hashboards?.find((board) => board.hashboardId === id));
  const first = selected[0];
  if (!first || !["none", "mac_address", "custom"].includes(first.workerNameMode)) return undefined;
  return selected.every(
    (board) =>
      board?.workerNameMode === first.workerNameMode &&
      (first.workerNameMode !== "custom" || board.workerName === first.workerName),
  )
    ? first
    : undefined;
}

export function WorkerNameDialog(props: Props) {
  const generation = useFleetStore((state) => state.auth.sessionGeneration);
  const permissions = usePermissions();
  if ((props.moduleIds || props.hashboardIds) && !hasMiningGroupPermissions(permissions, "pools")) return null;
  if (!permissions.includes("miner:update_worker_names") || !permissions.includes("pool:read")) return null;
  return <WorkerNameForm key={generation} {...props} />;
}

function WorkerNameForm(props: Props) {
  const { deviceSelector, moduleIds, hashboardIds, onDismiss } = props;
  const initial = currentWorkerName(props);
  const [password, setPassword] = useState("");
  const [mode, setMode] = useState(initial?.workerNameMode ?? "");
  const [custom, setCustom] = useState(initial?.workerNameMode === "custom" ? initial.workerName : "");
  const [review, setReview] = useState<ReviewWorkerNamesResponse>();
  const valid =
    mode !== "" && (mode !== "custom" || (custom.length > 0 && custom.length <= 100 && !/[\s\p{C}]/u.test(custom)));
  if (!password)
    return (
      <AuthenticateFleetModal
        open
        purpose="workerNames"
        onAuthenticated={(_username, value) => setPassword(value)}
        onDismiss={onDismiss}
      />
    );
  if (review)
    return (
      <WorkerNameReview
        review={review}
        password={password}
        onCancel={() => setReview(undefined)}
        onDone={(result) => {
          pushToast({
            message:
              result.failedCount || result.unknownCount || result.notSubmittedCount
                ? "Some worker name updates were not confirmed. Check pool assignment status."
                : `Submitted ${result.updatedCount} worker name updates`,
            status:
              result.failedCount || result.unknownCount || result.notSubmittedCount ? STATUSES.error : STATUSES.success,
          });
          onDismiss();
        }}
      />
    );
  return (
    <ActionModal
      title="Update worker names"
      confirm="Review changes"
      presentation="dialog"
      valid={valid}
      onDismiss={onDismiss}
      onSubmit={async (current) => {
        const workerName =
          mode === "custom"
            ? { mode: WorkerNameMode.CUSTOM, name: custom }
            : { mode: mode === "mac_address" ? WorkerNameMode.MAC_ADDRESS : WorkerNameMode.NONE };
        const next = await fleetManagementClient.reviewWorkerNames({
          deviceSelector,
          moduleIds,
          hashboardIds,
          workerName,
        });
        if (!current()) return;
        if (!next.reviews.length) {
          pushToast({
            message: next.failedCount ? "Some worker names could not be generated" : "Worker names are unchanged",
            status: next.failedCount ? STATUSES.error : STATUSES.success,
          });
          onDismiss();
        } else setReview(next);
      }}
    >
      <Select
        id="worker-name-option"
        label="Worker name"
        value={mode}
        placeholder="Select worker name"
        onChange={setMode}
        options={[
          { value: "none", label: "No worker name" },
          { value: "mac_address", label: "MAC address" },
          { value: "custom", label: "Custom worker name" },
        ]}
      />
      {mode === "custom" ? (
        <Input
          id="custom-worker-name"
          label="Custom worker name"
          initValue={custom}
          onChange={setCustom}
          maxLength={100}
        />
      ) : null}
    </ActionModal>
  );
}
