import { useCallback, useEffect, useMemo, useState } from "react";
import { getComponentOptions } from "../../componentOptions";
import { useTicketOperationKey } from "../../idempotency";
import { ticketAssetKey, ticketAssetLabel, ticketAssetSelection, ticketCategoryForAssets } from "../../ticketAssets";
import TicketAssetPicker from "../TicketAssetPicker";
import { type TicketAsset, TicketCategory } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { type TicketAssetPage, useMaintenanceApi } from "@/protoFleet/api/maintenance";
import { createAccountConnection } from "@/protoFleet/features/hostedAccounts/api";
import { useHostedAccounts } from "@/protoFleet/features/hostedAccounts/HostedAccountsContext";
import { useAccountQuery } from "@/protoFleet/features/hostedAccounts/useAccountQuery";
import { useMaintenanceOptions } from "@/protoFleet/features/maintenance/hooks/useMaintenanceOptions";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";
import { variants } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";
import Textarea from "@/shared/components/Textarea";

interface CreateTicketModalProps {
  onDismiss: () => void;
  onSuccess: () => void;
  prefill?: { alertId?: string; minerIdentifier?: string; component?: string; description?: string; siteId?: string };
}
const CATEGORY_OPTIONS = [
  { value: "miner", label: "Miner" },
  { value: "infrastructure", label: "Infrastructure" },
  { value: "other", label: "Other" },
];
type Category = "miner" | "infrastructure" | "other";
const CATEGORY_ENUMS = {
  miner: TicketCategory.MINER,
  infrastructure: TicketCategory.INFRASTRUCTURE,
  other: TicketCategory.OTHER,
};

const CreateTicketModal = ({ onDismiss, onSuccess, prefill }: CreateTicketModalProps) => {
  const activeSite = useFleetStore((state) => state.ui.activeSite);
  const { createTicket, listTicketAssets } = useMaintenanceApi();
  const directory = useHostedAccounts();
  const rootId = directory?.accounts.find((account) => account.depth === 0)?.accountId;
  const connection = useMemo(() => (rootId ? createAccountConnection(rootId) : null), [rootId]);
  useEffect(() => {
    connection?.activate();
    return () => connection?.dispose();
  }, [connection]);
  const loadClients = useCallback(
    async (signal: AbortSignal) =>
      connection && rootId ? connection.client.listChildren({ accountId: rootId }, { signal }) : { accounts: [] },
    [connection, rootId],
  );
  const clients = useAccountQuery(loadClients, !!rootId);
  const [clientAccountIds, setClientAccountIds] = useState<string[]>([]);
  const [clientIdsText, setClientIdsText] = useState("");
  const [targets, setTargets] = useState<TicketAsset[]>([]);
  const [assetPicker, setAssetPicker] = useState(false);
  const { keyFor: operationKey } = useTicketOperationKey();
  const options = useMaintenanceOptions();
  const [category, setCategory] = useState<Category>("miner");
  const [component, setComponent] = useState(prefill?.component || "Unknown");
  const [minerIdentifier, setMinerIdentifier] = useState(prefill?.minerIdentifier ?? "");
  const [description, setDescription] = useState(prefill?.description ?? "");
  const [siteId, setSiteId] = useState("");
  const [assigneeId, setAssigneeId] = useState("");
  const [urgent, setUrgent] = useState(false);
  const [shareWithClients, setShareWithClients] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const components = getComponentOptions(category, component);
  const siteOptions = useMemo(
    () => options.manageableSites.map((site) => ({ value: site.id, label: site.name })),
    [options.manageableSites],
  );
  const defaultSiteId = useMemo(() => {
    const preferredSiteId = prefill?.siteId ?? (activeSite.kind === "site" ? activeSite.id : undefined);
    return (
      options.manageableSites.find((site) => site.id === preferredSiteId)?.id ?? options.manageableSites[0]?.id ?? ""
    );
  }, [activeSite, options.manageableSites, prefill?.siteId]);
  const resolvedSiteId = options.manageableSites.some((site) => site.id === siteId) ? siteId : defaultSiteId;
  const loadAssets = useCallback(
    async (search: string, signal: AbortSignal, pageToken: string) =>
      new Promise<TicketAssetPage>((resolve, reject) => {
        void listTicketAssets({
          siteIds: options.manageableSites.map((site) => BigInt(site.id)),
          includeUnassigned: true,
          search,
          pageToken,
          signal,
          onSuccess: resolve,
          onError: (message) => reject(new Error(message)),
        });
      }),
    [listTicketAssets, options.manageableSites],
  );
  const explicitClientIds = rootId ? clientAccountIds : clientIdsText.split(/[,\s]+/).filter(Boolean);
  const assetFree = category !== "miner";
  const canSubmit = Boolean(
    description.trim() &&
    (assetFree ? resolvedSiteId : targets.length || minerIdentifier.trim()) &&
    (!shareWithClients || category !== "other" || explicitClientIds.length) &&
    !submitting,
  );
  const assigneeOptions = useMemo(
    () => [
      { value: "", label: "Unassigned" },
      ...options.assignees.map((item) => ({ value: item.id, label: item.username })),
    ],
    [options.assignees],
  );
  const dismiss = useCallback(() => {
    if (!submitting) onDismiss();
  }, [onDismiss, submitting]);
  const submit = useCallback(async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    setError(null);
    const payload = {
      shareWithClients,
      description: description.trim(),
      category: category === "miner" && targets.length ? ticketCategoryForAssets(targets) : CATEGORY_ENUMS[category],
      component,
      urgent,
      minerIdentifier: category === "miner" && !targets.length ? minerIdentifier.trim() : undefined,
      targets: targets.map(ticketAssetSelection),
      clientAccountIds: assetFree ? explicitClientIds : [],
      siteId: assetFree && resolvedSiteId ? BigInt(resolvedSiteId) : undefined,
      assigneeUserId: assigneeId ? BigInt(assigneeId) : undefined,
      alertId: prefill?.alertId,
    };
    await createTicket({
      ...payload,
      idempotencyKey: operationKey(payload),
      onSuccess: () => onSuccess(),
      onError: setError,
      onFinally: () => setSubmitting(false),
    });
  }, [
    assigneeId,
    canSubmit,
    category,
    component,
    createTicket,
    description,
    minerIdentifier,
    onSuccess,
    prefill?.alertId,
    resolvedSiteId,
    urgent,
    shareWithClients,
    targets,
    assetFree,
    explicitClientIds,
    operationKey,
  ]);
  return (
    <>
      <Modal
        open
        onDismiss={dismiss}
        title="New ticket"
        buttons={[
          {
            text: "Create ticket",
            variant: variants.primary,
            onClick: () => void submit(),
            disabled: !canSubmit,
            loading: submitting,
            dismissModalOnClick: false,
          },
        ]}
      >
        <div className="flex flex-col gap-3">
          <div className="grid grid-cols-2 gap-3">
            <Select
              id="category"
              label="Category"
              options={CATEGORY_OPTIONS}
              value={category}
              onChange={(value) => {
                setCategory(value as Category);
                setComponent("Unknown");
                setTargets([]);
                setMinerIdentifier("");
              }}
              forceBelow
            />
            <Select
              id="component"
              label="Component"
              options={components}
              value={component}
              onChange={setComponent}
              forceBelow
            />
          </div>
          {category === "miner" ? (
            <div className="flex flex-col gap-2">
              {targets.length ? (
                targets.map((asset) => <p key={ticketAssetKey(asset)}>{ticketAssetLabel(asset)}</p>)
              ) : (
                <Input id="miner-id" label="Miner ID" initValue={minerIdentifier} onChange={setMinerIdentifier} />
              )}
              <button type="button" className="text-left text-300 underline" onClick={() => setAssetPicker(true)}>
                Select affected equipment
              </button>
            </div>
          ) : (
            <Select
              id="site"
              label="Site"
              options={siteOptions}
              value={resolvedSiteId}
              onChange={setSiteId}
              forceBelow
            />
          )}
          {assetFree ? (
            rootId ? (
              <fieldset className="flex flex-col gap-2">
                <legend className="text-300">Affected clients (optional)</legend>
                {clients.error ? <div role="alert">{clients.error}</div> : null}
                {clients.data?.accounts.map((account) => (
                  <label key={account.accountId} className="flex items-center gap-3">
                    <Checkbox
                      checked={clientAccountIds.includes(account.accountId)}
                      onChange={(event) =>
                        setClientAccountIds((ids) =>
                          event.target.checked
                            ? [...ids, account.accountId]
                            : ids.filter((id) => id !== account.accountId),
                        )
                      }
                    />
                    {account.name}
                  </label>
                ))}
              </fieldset>
            ) : (
              <Input
                id="infra-client-ids"
                label="Affected client account IDs (optional, comma-separated)"
                initValue={clientIdsText}
                onChange={setClientIdsText}
              />
            )
          ) : null}
          <Select
            id="ticket-visibility"
            label="Visibility"
            value={shareWithClients ? "shared" : "internal"}
            options={[
              { value: "internal", label: "Internal — host team only" },
              { value: "shared", label: "Shared with affected clients" },
            ]}
            onChange={(value) => setShareWithClients(value === "shared")}
          />
          <Textarea
            id="host-ticket-description"
            label="Description"
            required
            maxLength={4096}
            initValue={description}
            onChange={setDescription}
            rows={3}
          />
          <Select
            id="assignee"
            label="Assignee"
            options={assigneeOptions}
            value={assigneeId}
            onChange={setAssigneeId}
            forceBelow
          />
          <label className="flex items-center gap-2 text-300">
            <Checkbox checked={urgent} onChange={(event) => setUrgent(event.target.checked)} />
            Mark as urgent
          </label>
          {error ? <div role="alert">{error}</div> : null}
        </div>
      </Modal>
      {assetPicker ? (
        <TicketAssetPicker
          singleParent
          initialSelected={targets}
          load={loadAssets}
          onDismiss={() => setAssetPicker(false)}
          onSelect={(assets) => {
            setTargets(assets);
            setMinerIdentifier("");
            setAssetPicker(false);
          }}
        />
      ) : null}
    </>
  );
};
export default CreateTicketModal;
