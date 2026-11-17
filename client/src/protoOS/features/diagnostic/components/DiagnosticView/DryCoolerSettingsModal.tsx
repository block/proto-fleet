import { useState } from "react";

import type { MinerAdapter } from "@/protoOS/api/adapters/types";
import { dryCoolerWriteHold } from "@/protoOS/features/containerHome/hooks/useFanReversalOutcome";
import { useDryCoolerHold } from "@/protoOS/features/containerHome/hooks/useFanReversalReadback";
import { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import SegmentedControl from "@/shared/components/SegmentedControl";
import { pushToast, updateToast } from "@/shared/features/toaster";

type DryCoolerMode = "auto" | "manual";

interface DryCoolerSettingsModalProps {
  adapter: MinerAdapter;
  initialMode: DryCoolerMode;
  initialTargetC?: number;
  onDismiss: () => void;
  onSaved: () => Promise<void> | void;
}

const numericInput = (value: string) => value.replace(/[^0-9.-]/g, "");

const DryCoolerSettingsModal = ({
  adapter,
  initialMode,
  initialTargetC,
  onDismiss,
  onSaved,
}: DryCoolerSettingsModalProps) => {
  const [mode, setMode] = useState<DryCoolerMode>(initialMode);
  const [target, setTarget] = useState(initialTargetC?.toString() ?? "");
  const [pending, setPending] = useState(false);
  const authRetry = useAuthRetry();
  const targetValue = Number(target);
  const validTarget = target.trim() !== "" && Number.isFinite(targetValue);
  // The PLC owns the dry cooler during a fan reversal, wherever the run was started.
  const coolerHold = useDryCoolerHold();
  const canSave = mode === "auto" && validTarget && !pending && !coolerHold;

  const save = async () => {
    if (!canSave || !adapter.setContainerControl || !adapter.setContainerSetpoint) return;
    // Checked where the writes are sent, in case the hold began after this render.
    if (dryCoolerWriteHold("dry_cooler_auto")) return;
    const setTarget = adapter.setContainerSetpoint.bind(adapter);
    const setControl = adapter.setContainerControl.bind(adapter);
    const generation = useMinerStore.getState().connectionGeneration;
    const isCurrent = () => useMinerStore.getState().connectionGeneration === generation;
    setPending(true);
    const toast = pushToast({ message: "Updating dry cooler settings…", status: "loading", ttl: false });
    let targetSaved = false;
    let saveError: string | undefined;
    await authRetry({
      shouldRetry: isCurrent,
      request: async (params) => {
        if (!isCurrent()) return;
        // Preserve completed writes when authentication retries the sequence.
        if (!targetSaved) {
          await setTarget("dry_cooler_auto_temp_c", targetValue, params);
          targetSaved = true;
        }
        if (!isCurrent()) return;
        await setControl("dry_cooler_auto", true, params);
      },
      onError: (error) => {
        const message =
          (error as { error?: { message?: string } } | undefined)?.error?.message ||
          (error instanceof Error ? error.message : "The request did not complete");
        saveError = targetSaved
          ? `Target temperature saved, but automatic mode could not be confirmed: ${message}`
          : `Could not confirm the target temperature was saved: ${message}`;
      },
    });
    if (!isCurrent()) {
      updateToast(toast, {
        message: "Device changed. Check the previous device’s dry cooler settings before trying again.",
        status: "error",
        ttl: 6000,
      });
      setPending(false);
      return;
    }

    let refreshFailed = false;
    if (targetSaved) {
      try {
        await onSaved();
      } catch {
        refreshFailed = true;
      }
    }
    if (saveError) {
      updateToast(toast, {
        message: `${saveError}${refreshFailed ? ". Readings could not be refreshed." : ""}`,
        status: "error",
        ttl: 6000,
      });
      setPending(false);
      return;
    }
    updateToast(toast, {
      message: refreshFailed
        ? "Dry cooler settings saved, but readings could not be refreshed. No need to save again."
        : "Dry cooler settings updated",
      status: refreshFailed ? "error" : "success",
      ttl: refreshFailed ? 6000 : 3000,
    });
    onDismiss();
  };

  const saveButton = {
    text: "Save",
    variant: variants.primary,
    disabled: !canSave,
    loading: pending,
    dismissModalOnClick: false,
    onClick: save,
  };

  return (
    <Modal
      open
      title="Manage dry cooler settings"
      dialogLabel="Manage dry cooler settings"
      onDismiss={pending ? undefined : onDismiss}
      buttons={[saveButton]}
      phoneFooterButtons={[saveButton]}
      divider={false}
      surfaceClassName="tablet:w-[min(calc(100vw-(--spacing(4))),728px)]"
    >
      <div className="space-y-8 text-text-primary">
        {coolerHold ? <p className="text-200 text-text-primary-70">{coolerHold}</p> : null}
        <div>
          <div className="mb-3 text-emphasis-300">Mode</div>
          <SegmentedControl
            segments={[
              { key: "auto", title: "Auto" },
              { key: "manual", title: "Manual" },
            ]}
            initialSegmentKey={initialMode}
            onSelect={(key) => setMode(key as DryCoolerMode)}
          />
        </div>

        {mode === "auto" ? (
          <div>
            <div className="mb-3 text-emphasis-300">Inlet temperature settings</div>
            <Input
              id="dry-cooler-target-temperature"
              label="Target °C"
              initValue={target}
              disabled={pending}
              inputMode="decimal"
              sanitize={numericInput}
              onChange={setTarget}
              error={target.trim() !== "" && !validTarget ? "Enter a valid temperature" : false}
              tooltip={{ body: "The dry cooler automatically adjusts to maintain this inlet temperature." }}
            />
          </div>
        ) : (
          <div>
            <div className="mb-3 text-emphasis-300">Settings</div>
            <div className="grid gap-3 tablet:grid-cols-2">
              <Input id="dry-cooler-pump-speed" label="Pump speed (RPM)" disabled />
              <Input id="dry-cooler-fan-speed" label="Fan speed (RPM)" disabled />
            </div>
          </div>
        )}
      </div>
    </Modal>
  );
};

export default DryCoolerSettingsModal;
