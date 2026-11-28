import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  type ControllerSupport,
  frigateCodeText,
  timestampSeconds,
  UPDATABLE_KIND_RULES,
  type UpdatableKind,
} from "./frigateUpdate";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import {
  type FrigatePackage,
  type GetSupportResponse,
  type InstanceSupport,
  type UpdateJob,
} from "@/protoFleet/api/generated/frigateupdate/v1/frigateupdate_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useFrigatePackages } from "@/protoFleet/api/useFrigatePackages";
import { useFrigateUpdateActions } from "@/protoFleet/api/useFrigateUpdates";
import { useTicketOperationKey } from "@/protoFleet/features/maintenance/idempotency";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Dialog from "@/shared/components/Dialog";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Select from "@/shared/components/Select";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

const GENERIC_INELIGIBLE = "This container can't be updated right now.";

function packageLabel(item: FrigatePackage) {
  return `${item.version} (${item.architecture})`;
}

function packageDescription(item: FrigatePackage) {
  const uploaded = formatTimestamp(timestampSeconds(item.uploadedAt));
  return uploaded ? `${item.filename}, uploaded ${uploaded}` : item.filename;
}

type Loaded<T> = { status: "loading" } | { status: "error"; message: string } | { status: "ready"; value: T };

export default function FrigateUpdateDialog({
  container,
  kind,
  onClose,
  onStarted,
}: {
  container: Pick<Container, "deviceIdentifier" | "name">;
  kind: UpdatableKind;
  onClose: () => void;
  onStarted?: (job: UpdateJob | undefined) => void;
}) {
  const { deviceIdentifier, name } = container;
  const rules = UPDATABLE_KIND_RULES[kind];
  const { listPackages } = useFrigatePackages();
  const { getSupport, startUpdate } = useFrigateUpdateActions();
  const { keyFor, reset } = useTicketOperationKey();
  const [packages, setPackages] = useState<Loaded<FrigatePackage[]>>({ status: "loading" });
  // Packages uploaded to another Fleet host can't be staged from this one.
  const [elsewhereCount, setElsewhereCount] = useState(0);
  const [packageId, setPackageId] = useState("");
  const [support, setSupport] = useState<Loaded<GetSupportResponse>>({ status: "loading" });
  const [recheck, setRecheck] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const inFlight = useRef(false);

  useEffect(() => {
    const abort = new AbortController();
    listPackages(abort.signal)
      .then((all) => {
        if (abort.signal.aborted) return;
        // The server refuses a package of another kind, so it is never offered.
        const items = all.filter((item) => item.kind === kind);
        const usable = items.filter((item) => item.storedOnThisHost);
        setElsewhereCount(items.length - usable.length);
        setPackages({ status: "ready", value: usable });
        // A single package is the only possible choice; with several, the operator picks.
        if (usable.length === 1) setPackageId(usable[0].id);
      })
      .catch((cause) => {
        if (!abort.signal.aborted) {
          setPackages({ status: "error", message: getErrorMessage(cause, "Couldn't load packages.") });
        }
      });
    return () => abort.abort();
  }, [kind, listPackages]);

  // Readiness is read once without a package, then again for the chosen package
  // so architecture, signing key and version problems show before starting.
  useEffect(() => {
    const abort = new AbortController();
    // eslint-disable-next-line react-hooks/set-state-in-effect -- show the loading state while the controllers are re-read
    setSupport({ status: "loading" });
    getSupport(deviceIdentifier, packageId || undefined, abort.signal)
      .then((value) => {
        if (!abort.signal.aborted) setSupport({ status: "ready", value });
      })
      .catch((cause) => {
        if (!abort.signal.aborted) {
          setSupport({ status: "error", message: getErrorMessage(cause, "Couldn't check the controllers.") });
        }
      });
    return () => abort.abort();
  }, [getSupport, deviceIdentifier, packageId, recheck]);

  const options = useMemo(
    () =>
      packages.status === "ready"
        ? packages.value.map((item) => ({
            value: item.id,
            label: packageLabel(item),
            description: packageDescription(item),
          }))
        : [],
    [packages],
  );

  const eligible = support.status === "ready" && support.value.eligible;
  const canStart = !!packageId && eligible && !busy;
  // No signer key ID means Fleet stored the package without verifying its signature.
  const chosen = packages.status === "ready" ? packages.value.find((item) => item.id === packageId) : undefined;
  const signatureUnverified = !!chosen && !chosen.releaseKeyId;

  const submit = useCallback(async () => {
    if (inFlight.current || !canStart) return;
    inFlight.current = true;
    setBusy(true);
    setError(undefined);
    try {
      // One key per operator action: the same package for the same container
      // keeps its key when the operator resubmits after a failure.
      const job = await startUpdate({
        deviceIdentifier,
        packageId,
        idempotencyKey: keyFor({ deviceIdentifier, packageId }),
      });
      reset();
      pushToast({ message: `${name}: ${rules.noun} update started.`, status: STATUSES.success });
      onStarted?.(job);
      onClose();
    } catch (cause) {
      setError(getErrorMessage(cause, "Couldn't start the update."));
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }, [canStart, deviceIdentifier, keyFor, name, onClose, onStarted, packageId, reset, rules.noun, startUpdate]);

  return (
    <Dialog
      open
      title={`Update ${rules.noun}: ${name}`}
      onDismiss={busy ? undefined : onClose}
      testId="frigate-update-dialog"
      buttons={[
        { text: "Cancel", variant: variants.secondary, onClick: onClose, disabled: busy },
        {
          text: busy ? "Starting…" : "Start update",
          variant: variants.primary,
          onClick: () => void submit(),
          disabled: !canStart,
        },
      ]}
    >
      <div className="flex flex-col gap-4">
        <Callout
          intent="warning"
          prefixIcon={<Alert />}
          title={rules.startWarning.title}
          subtitle={rules.startWarning.subtitle}
          testId="frigate-update-disruption"
        />
        {packages.status === "error" ? (
          <p role="alert" className="text-intent-critical-text">
            {packages.message}
          </p>
        ) : null}
        {packages.status === "ready" && packages.value.length === 0 && elsewhereCount === 0 ? (
          <p role="status">No {rules.noun} package is uploaded. Upload one in Settings, Firmware.</p>
        ) : null}
        {packages.status === "ready" && elsewhereCount > 0 ? (
          <p role="status">
            {elsewhereCount === 1 ? "1 package is" : `${elsewhereCount} packages are`} stored on another Fleet host and
            can't be used here. Upload {elsewhereCount === 1 ? "it" : "them"} again in Settings, Firmware.
          </p>
        ) : null}
        {packages.status === "ready" && packages.value.length > 0 ? (
          <Select
            id="frigate-update-package"
            label="Package"
            options={options}
            value={packageId}
            onChange={setPackageId}
            placeholder="Choose a package"
            disabled={busy}
            forceBelow
          />
        ) : null}
        {signatureUnverified ? (
          <p role="status">
            Fleet did not verify this package's signature. The controllers verify it before they install.
          </p>
        ) : null}
        <SupportSummary
          support={support}
          describe={rules.support}
          showVerdict={!rules.verdictNeedsPackage || !!packageId}
          onCheckAgain={() => setRecheck((value) => value + 1)}
        />
        {error ? (
          <p role="alert" className="text-intent-critical-text">
            {error}
          </p>
        ) : null}
      </div>
    </Dialog>
  );
}

function SupportSummary({
  support,
  describe,
  showVerdict,
  onCheckAgain,
}: {
  support: Loaded<GetSupportResponse>;
  describe: (instance: InstanceSupport) => ControllerSupport;
  showVerdict: boolean;
  onCheckAgain: () => void;
}) {
  if (support.status === "loading") {
    return (
      <div role="status" className="flex items-center gap-2 text-300 text-text-primary-70">
        <ProgressCircular indeterminate size={16} />
        Checking the controllers…
      </div>
    );
  }
  if (support.status === "error") {
    return (
      <Callout
        intent="danger"
        prefixIcon={<Alert />}
        title="Couldn't check the controllers"
        subtitle={support.message}
        buttonText="Check again"
        buttonOnClick={onCheckAgain}
        testId="frigate-update-support-error"
      />
    );
  }
  const { eligible, ineligibleCode, ineligibleReason, instances } = support.value;
  const ineligibleText = frigateCodeText(ineligibleCode);
  // A mapped code gives the headline; the server's reason keeps the specifics
  // it names (which controller, how many bytes free) that the rows don't show.
  const ineligibleDetail =
    [ineligibleText ? ineligibleReason : "", ineligibleCode ? `Code: ${ineligibleCode}` : ""]
      .filter(Boolean)
      .join(" ") || undefined;
  return (
    <div className="flex flex-col gap-3">
      {!eligible && showVerdict ? (
        <Callout
          intent="warning"
          prefixIcon={<Alert />}
          title={ineligibleText ?? (ineligibleReason || GENERIC_INELIGIBLE)}
          subtitle={ineligibleDetail}
          buttonText="Check again"
          buttonOnClick={onCheckAgain}
          testId="frigate-update-ineligible"
        />
      ) : null}
      <ul aria-label="Controllers" className="flex flex-col gap-2">
        {instances.map((instance) => {
          const { summary, problems, note } = describe(instance);
          return (
            <li key={instance.instanceId} className="rounded-lg border border-border-5 p-3">
              <div className="text-emphasis-300 text-text-primary">{instance.instanceId}</div>
              <div className="text-200 text-text-primary-70">{summary}</div>
              {problems.length > 0 ? (
                <ul className="mt-1 list-disc pl-5 text-200 text-intent-critical-text">
                  {problems.map((problem) => (
                    <li key={problem}>{problem}</li>
                  ))}
                </ul>
              ) : (
                <div className="text-200 text-text-primary-70">Ready</div>
              )}
              {note ? <div className="text-200 text-text-primary-70">{note}</div> : null}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
