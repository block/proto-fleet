import { useCallback, useEffect, useRef, useState } from "react";

import { ticketAssetKey, ticketAssetLabel, ticketAssetSelection } from "../../ticketAssets";
import type { TicketDetail } from "../../types";
import TicketAssetPicker from "../TicketAssetPicker";
import {
  MinerIdentifierType,
  type MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { type TicketAsset, TicketCategory } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { lookupMinerByIdentifier } from "@/protoFleet/api/lookupMinerByIdentifier";
import { type TicketAssetPage, useMaintenanceApi } from "@/protoFleet/api/maintenance";
import Button, { sizes } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Textarea from "@/shared/components/Textarea";

const validId = (value: string) => !value || /^[1-9]\d*$/.test(value);
const MinerTargetLink = ({
  asset,
  disabled,
  onOpenMiner,
}: {
  asset: TicketAsset;
  disabled: boolean;
  onOpenMiner: (miner: MinerStateSnapshot) => void;
}) => {
  const [snapshot, setSnapshot] = useState<MinerStateSnapshot | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const controller = useRef<AbortController | null>(null);
  const navigationDisabled = useRef(disabled);
  useEffect(() => {
    navigationDisabled.current = disabled;
  }, [disabled]);
  useEffect(() => () => controller.current?.abort(), []);
  const openMiner = () => {
    if (disabled || loading) return;
    if (snapshot) {
      onOpenMiner(snapshot);
      return;
    }
    const request = new AbortController();
    controller.current = request;
    setLoading(true);
    setError("");
    void lookupMinerByIdentifier(asset.minerIdentifier, MinerIdentifierType.DEVICE_IDENTIFIER, request.signal).then(
      (result) => {
        if (request.signal.aborted) return;
        setLoading(false);
        if (result.status === "found") {
          setSnapshot(result.snapshot);
          if (!navigationDisabled.current) onOpenMiner(result.snapshot);
        } else {
          setError(result.status === "notFound" ? "Miner is no longer available." : result.message);
        }
      },
    );
  };
  return (
    <div className="flex max-w-full min-w-0 flex-col gap-2">
      <Button
        variant="textOnly"
        className="max-w-full min-w-0 justify-start text-left"
        disabled={disabled}
        loading={loading}
        onClick={openMiner}
      >
        <span className="wrap-anywhere whitespace-normal">{ticketAssetLabel(asset)}</span>
      </Button>
      {error ? <p role="alert">{error}</p> : null}
    </div>
  );
};
const TargetEvidence = ({
  ticket,
  asset,
  onSaved,
  onCancel,
  disabled,
  submitMutation,
}: {
  ticket: TicketDetail;
  asset: TicketAsset;
  onSaved: () => void | Promise<void>;
  onCancel: () => void;
  disabled: boolean;
  submitMutation: (mutation: () => Promise<boolean>) => Promise<boolean>;
}) => {
  const { updateTarget } = useMaintenanceApi();
  const [diagnosis, setDiagnosis] = useState(asset.diagnosis);
  const [warrantyEvidence, setWarrantyEvidence] = useState(asset.warrantyEvidence);
  const [repairOutcome, setRepairOutcome] = useState(asset.repairOutcome);
  const [rmaOutcome, setRmaOutcome] = useState(asset.rmaOutcome);
  const [moduleModel, setModuleModel] = useState(
    asset.kind === TicketCategory.MODULE || asset.kind === TicketCategory.HASHBOARD ? asset.model : "",
  );
  const [replacementModuleId, setReplacementModuleId] = useState(asset.replacementModuleId?.toString() ?? "");
  const [replacementModuleModel, setReplacementModuleModel] = useState(asset.replacementModuleId ? asset.model : "");
  const [replacementHashboardId, setReplacementHashboardId] = useState(asset.replacementHashboardId?.toString() ?? "");
  const [busy, setBusy] = useState(false);
  const [version, setVersion] = useState(ticket.updatedAtSnapshot);
  const [error, setError] = useState("");
  const blocked = busy || disabled;
  const currentAsset = ticket.targets.find((item) => ticketAssetKey(item) === ticketAssetKey(asset));
  const versionChanged =
    version &&
    ticket.updatedAtSnapshot &&
    (version.seconds !== ticket.updatedAtSnapshot.seconds || version.nanos !== ticket.updatedAtSnapshot.nanos);
  const valid =
    !!version &&
    validId(replacementModuleId) &&
    validId(replacementHashboardId) &&
    (!replacementModuleId ||
      (moduleModel.trim() !== "" &&
        replacementModuleModel.trim() === moduleModel.trim() &&
        replacementModuleId !== String(asset.id))) &&
    (!replacementHashboardId || replacementHashboardId !== String(asset.id));
  return (
    <div className="flex flex-col gap-3 rounded-xl border border-border-5 p-4">
      <Textarea
        id="target-diagnosis"
        label="Diagnosis"
        initValue={diagnosis}
        onChange={setDiagnosis}
        maxLength={4096}
        rows={2}
        disabled={blocked}
      />
      <Textarea
        id="target-warranty"
        label="Warranty evidence"
        initValue={warrantyEvidence}
        onChange={setWarrantyEvidence}
        maxLength={4096}
        rows={2}
        disabled={blocked}
      />
      <Textarea
        id="target-repair"
        label="Repair outcome"
        initValue={repairOutcome}
        onChange={setRepairOutcome}
        maxLength={4096}
        rows={2}
        disabled={blocked}
      />
      <Textarea
        id="target-rma"
        label="RMA outcome"
        initValue={rmaOutcome}
        onChange={setRmaOutcome}
        maxLength={4096}
        rows={2}
        disabled={blocked}
      />
      {asset.kind === TicketCategory.MODULE || asset.kind === TicketCategory.HASHBOARD ? (
        <Input
          id="target-module-model"
          label="Actual module model"
          initValue={moduleModel}
          onChange={setModuleModel}
          maxLength={255}
          disabled={blocked}
        />
      ) : null}
      {asset.kind === TicketCategory.MODULE ? (
        <>
          <Input
            id="target-module-replacement"
            label="Replacement module ID (optional)"
            initValue={replacementModuleId}
            onChange={setReplacementModuleId}
            disabled={blocked}
          />
          {replacementModuleId ? (
            <Input
              id="target-replacement-model"
              label="Replacement module model"
              initValue={replacementModuleModel}
              onChange={setReplacementModuleModel}
              maxLength={255}
              disabled={blocked}
            />
          ) : null}
          {replacementModuleId ? (
            <p className="text-200 text-text-primary-70">
              Record a different physical module with the same actual model.
            </p>
          ) : null}
        </>
      ) : asset.kind === TicketCategory.HASHBOARD ? (
        <>
          <Input
            id="target-board-replacement"
            label="Replacement hashboard ID (optional)"
            initValue={replacementHashboardId}
            onChange={setReplacementHashboardId}
            disabled={blocked}
          />
          {replacementHashboardId ? (
            <p className="text-200 text-text-primary-70">
              The replacement must be a different physical board installed in module {asset.moduleId?.toString()}.
            </p>
          ) : null}
        </>
      ) : null}
      <p className="text-200 text-text-primary-50">Repair details are internal to the host.</p>
      {versionChanged ? (
        <div className="flex flex-col gap-2">
          <p>Ticket changed. Review the current repair details before confirming your draft again.</p>
          {currentAsset
            ? [
                ["Diagnosis", currentAsset.diagnosis],
                ["Warranty evidence", currentAsset.warrantyEvidence],
                ["Repair outcome", currentAsset.repairOutcome],
                ["RMA outcome", currentAsset.rmaOutcome],
                ["Module model", currentAsset.model],
                ["Replacement module ID", currentAsset.replacementModuleId?.toString()],
                ["Replacement hashboard ID", currentAsset.replacementHashboardId?.toString()],
              ]
                .filter(([, value]) => value)
                .map(([label, value]) => (
                  <p key={label} className="whitespace-pre-wrap">
                    Current {label?.toLowerCase()}: {value}
                  </p>
                ))
            : null}
          <Button
            text="Use refreshed repair version"
            variant="secondary"
            disabled={blocked || !currentAsset}
            onClick={() => setVersion(ticket.updatedAtSnapshot)}
          />
        </div>
      ) : null}
      {error ? <p role="alert">{error}</p> : null}
      <div className="flex justify-end gap-3">
        <Button text="Cancel" variant="secondary" size={sizes.compact} disabled={blocked} onClick={onCancel} />
        <Button
          text="Save repair details"
          variant="primary"
          size={sizes.compact}
          loading={busy}
          disabled={!valid || disabled}
          onClick={() => {
            if (!valid || !version || blocked) return;
            setBusy(true);
            setError("");
            void submitMutation(async () => {
              let ok = false;
              await updateTarget({
                ticketId: BigInt(ticket.id),
                kind: asset.kind,
                assetId: asset.id,
                expectedUpdatedAt: version,
                diagnosis: diagnosis.trim(),
                warrantyEvidence: warrantyEvidence.trim(),
                repairOutcome: repairOutcome.trim(),
                rmaOutcome: rmaOutcome.trim(),
                moduleModel: moduleModel.trim() || undefined,
                replacementModuleId: replacementModuleId ? BigInt(replacementModuleId) : undefined,
                replacementModuleModel: replacementModuleId ? replacementModuleModel.trim() : undefined,
                replacementHashboardId: replacementHashboardId ? BigInt(replacementHashboardId) : undefined,
                clearReplacementModule: asset.kind === TicketCategory.MODULE && !replacementModuleId,
                clearReplacementHashboard: asset.kind === TicketCategory.HASHBOARD && !replacementHashboardId,
                onSuccess: () => {
                  ok = true;
                },
                onError: setError,
              });
              if (ok) await onSaved();
              return ok;
            })
              .then((ok) => {
                if (ok) onCancel();
              })
              .finally(() => setBusy(false));
          }}
        />
      </div>
    </div>
  );
};

const TicketTargets = ({
  ticket,
  canManage,
  canReadMiners,
  disabled = false,
  onOpenMiner,
  onSaved,
  submitMutation = (mutation) => mutation(),
}: {
  ticket: TicketDetail;
  canManage: boolean;
  canReadMiners: boolean;
  disabled?: boolean;
  onOpenMiner: (miner: MinerStateSnapshot) => void;
  onSaved: () => void | Promise<void>;
  submitMutation?: (mutation: () => Promise<boolean>) => Promise<boolean>;
}) => {
  const { listTicketAssets, updateTicket } = useMaintenanceApi();
  const [editing, setEditing] = useState<TicketAsset | null>(null);
  const [picking, setPicking] = useState(false);
  const [version, setVersion] = useState(ticket.updatedAtSnapshot);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const blocked = busy || disabled;
  const loadBoards = useCallback(
    async (search: string, signal: AbortSignal, pageToken: string) =>
      new Promise<TicketAssetPage>((resolve, reject) => {
        void listTicketAssets({
          ticketId: BigInt(ticket.id),
          search,
          pageToken,
          signal,
          onSuccess: resolve,
          onError: (message) => reject(new Error(message)),
        });
      }),
    [listTicketAssets, ticket.id],
  );
  const sectionTitle = ticket.targets.length ? "Affected equipment" : "Affected clients";
  return (
    <section className="flex flex-col gap-3" aria-label={sectionTitle}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 className="text-emphasis-300 font-medium">{sectionTitle}</h3>
        {canManage && ticket.targets.some((asset) => asset.kind === TicketCategory.MODULE || asset.moduleId) ? (
          <Button
            text="Add boards"
            variant="secondary"
            size={sizes.compact}
            disabled={blocked}
            onClick={() => {
              setVersion(ticket.updatedAtSnapshot);
              setPicking(true);
            }}
          />
        ) : null}
      </div>
      {ticket.targets.map((asset) => (
        <div key={ticketAssetKey(asset)} className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center justify-between gap-3">
            {canReadMiners && asset.kind === TicketCategory.MINER && asset.minerIdentifier ? (
              <MinerTargetLink key={asset.minerIdentifier} asset={asset} disabled={blocked} onOpenMiner={onOpenMiner} />
            ) : (
              <span>{ticketAssetLabel(asset)}</span>
            )}
            {canManage ? (
              <Button
                text="Edit repair details"
                ariaLabel={`Edit repair details for ${asset.label || asset.id}`}
                variant="secondary"
                size={sizes.compact}
                disabled={blocked}
                onClick={() => setEditing(asset)}
              />
            ) : null}
          </div>
          {asset.replacementModuleId ? <p>Replacement module ID: {asset.replacementModuleId.toString()}</p> : null}
          {asset.replacementHashboardId ? (
            <p>Replacement hashboard ID: {asset.replacementHashboardId.toString()}</p>
          ) : null}
          {!editing || ticketAssetKey(editing) !== ticketAssetKey(asset)
            ? [
                ["Diagnosis", asset.diagnosis],
                ["Warranty evidence", asset.warrantyEvidence],
                ["Repair outcome", asset.repairOutcome],
                ["RMA outcome", asset.rmaOutcome],
              ]
                .filter(([, value]) => value)
                .map(([label, value]) => (
                  <p key={label} className="text-300 whitespace-pre-wrap">
                    {label}: {value}
                  </p>
                ))
            : null}
          {editing && ticketAssetKey(editing) === ticketAssetKey(asset) ? (
            <TargetEvidence
              ticket={ticket}
              asset={editing}
              onSaved={onSaved}
              onCancel={() => setEditing(null)}
              disabled={blocked}
              submitMutation={submitMutation}
            />
          ) : null}
        </div>
      ))}
      {!ticket.targets.length && ticket.audiences.length ? (
        <p>Clients: {ticket.audiences.map((audience) => audience.name).join(", ")}</p>
      ) : null}
      {error ? <p role="alert">{error}</p> : null}
      {picking ? (
        <TicketAssetPicker
          title="Add affected boards"
          load={loadBoards}
          onDismiss={() => setPicking(false)}
          onSelect={(assets) => {
            if (!version || blocked) return;
            setPicking(false);
            setBusy(true);
            setError("");
            void submitMutation(async () => {
              let ok = false;
              await updateTicket({
                id: BigInt(ticket.id),
                expectedUpdatedAt: version,
                affectedBoards: assets.map(ticketAssetSelection),
                onSuccess: () => {
                  ok = true;
                },
                onError: setError,
              });
              if (ok) await onSaved();
              return ok;
            }).finally(() => setBusy(false));
          }}
        />
      ) : null}
    </section>
  );
};
export default TicketTargets;
