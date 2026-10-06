import { useCallback, useEffect, useMemo, useState } from "react";
import type { MinerModelGroup } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import useMinerModelGroups from "@/protoFleet/api/useMinerModelGroups";

const MINER_MODELS_ERROR_MESSAGE = "Couldn't load fleet miner models.";

interface UseMinerTargetOptionsArgs {
  /** Fetch model groups while true (typically the dialog's open state). */
  active: boolean;
  /** Currently selected manufacturer; model options are limited to it. */
  selectedManufacturer: string;
  /** Currently selected model, reconciled against the options as `resolvedModel`. */
  selectedModel: string;
  /** Existing values kept selectable even when absent from the fleet. */
  seedManufacturer?: string;
  seedModel?: string;
}

/**
 * Loads the fleet's miner model groups and builds the Manufacturer/Model
 * select options shared by the firmware metadata dialogs. `reset` clears the
 * fetched groups so the next activation refetches.
 *
 * Because `modelOptions` narrows to the selected manufacturer and arrives
 * asynchronously, `selectedModel` can fall out of the list. This hook owns that
 * invariant: use `resolvedModel` rather than the raw selection so a stale model
 * is never submitted or rendered as a phantom option.
 */
export function useMinerTargetOptions({
  active,
  selectedManufacturer,
  selectedModel,
  seedManufacturer = "",
  seedModel = "",
}: UseMinerTargetOptionsArgs) {
  const { getMinerModelGroups } = useMinerModelGroups();
  const [modelGroups, setModelGroups] = useState<MinerModelGroup[] | null>(null);
  const [modelsError, setModelsError] = useState<string | null>(null);

  useEffect(() => {
    if (!active || modelGroups !== null || modelsError !== null) return;
    let cancelled = false;
    void getMinerModelGroups(null)
      .then((groups) => {
        if (!cancelled) setModelGroups(groups);
      })
      .catch(() => {
        if (!cancelled) {
          setModelGroups([]);
          setModelsError(MINER_MODELS_ERROR_MESSAGE);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [active, getMinerModelGroups, modelGroups, modelsError]);

  const reset = useCallback(() => {
    setModelGroups(null);
    setModelsError(null);
  }, []);

  const manufacturerOptions = useMemo(() => {
    const manufacturers = new Set((modelGroups ?? []).map((group) => group.manufacturer.trim()).filter(Boolean));
    if (seedManufacturer.trim()) manufacturers.add(seedManufacturer.trim());
    return [
      { value: "", label: "Select manufacturer" },
      ...[...manufacturers].sort().map((manufacturer) => ({ value: manufacturer, label: manufacturer })),
    ];
  }, [modelGroups, seedManufacturer]);

  const modelOptions = useMemo(() => {
    const models = new Set(
      (modelGroups ?? [])
        .filter((group) => group.manufacturer.trim() === selectedManufacturer.trim())
        .map((group) => group.model.trim())
        .filter(Boolean),
    );
    const selectedManufacturerKey = selectedManufacturer.trim().toLowerCase();
    const seedManufacturerKey = seedManufacturer.trim().toLowerCase();
    if (seedModel.trim() && selectedManufacturerKey === seedManufacturerKey) models.add(seedModel.trim());
    return [
      { value: "", label: "Select model" },
      ...[...models].sort().map((model) => ({ value: model, label: model })),
    ];
  }, [modelGroups, seedManufacturer, seedModel, selectedManufacturer]);

  const resolvedModel = useMemo(
    () => (modelOptions.some((option) => option.value === selectedModel) ? selectedModel : ""),
    [modelOptions, selectedModel],
  );

  return { modelGroups, modelsError, manufacturerOptions, modelOptions, resolvedModel, reset };
}
