import { useCallback, useEffect, useState } from "react";

import InfraLocationFields from "@/protoFleet/features/infrastructure/components/InfraLocationFields";
import {
  DEFAULT_DRIVER_TYPE,
  type DriverFormValues,
  driverTypeOptions,
  getDriverFormModule,
} from "@/protoFleet/features/infrastructure/driverForms";
import type {
  InfraBuildingOption,
  InfraDeviceDraft,
  InfraDeviceKind,
  InfraRackOption,
} from "@/protoFleet/features/infrastructure/types";
import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";

const deviceKindOptions: { value: InfraDeviceKind; label: string }[] = [
  { value: "single_fan", label: "Single fan" },
  { value: "fan_group", label: "Fan group" },
];

export interface ManualAddStepState {
  canAdd: boolean;
  addHandler: () => void;
}

interface ManualAddStepProps {
  siteOptions?: string[];
  buildingOptions?: InfraBuildingOption[];
  rackOptions?: InfraRackOption[];
  initialSiteName?: string;
  onSuccess: (device: InfraDeviceDraft) => void;
  onStateChange: (state: ManualAddStepState) => void;
}

const ManualAddStep = ({
  siteOptions = [],
  buildingOptions = [],
  rackOptions = [],
  initialSiteName,
  onSuccess,
  onStateChange,
}: ManualAddStepProps) => {
  const [name, setName] = useState("");
  const [site, setSite] = useState(initialSiteName ?? "");
  const [building, setBuilding] = useState("");
  const [rack, setRack] = useState("");
  const [deviceKind, setDeviceKind] = useState<InfraDeviceKind>("single_fan");
  const [fanCount, setFanCount] = useState("1");
  const [driverType, setDriverType] = useState(DEFAULT_DRIVER_TYPE);
  const [driverValues, setDriverValues] = useState<DriverFormValues>(
    () => getDriverFormModule(DEFAULT_DRIVER_TYPE)?.emptyValues() ?? {},
  );

  const driverFormModule = getDriverFormModule(driverType);
  const fanCountNumber = deviceKind === "single_fan" ? 1 : Number(fanCount);
  const isFanCountValid = deviceKind === "single_fan" || (Number.isInteger(fanCountNumber) && fanCountNumber > 1);
  const isValid =
    [name, site, building].every((value) => value.trim().length > 0) &&
    isFanCountValid &&
    driverFormModule !== undefined &&
    driverFormModule.isValid(driverValues);

  const handleDeviceKindChange = useCallback((value: string) => {
    const nextDeviceKind = value as InfraDeviceKind;
    setDeviceKind(nextDeviceKind);
    if (nextDeviceKind === "single_fan") {
      setFanCount("1");
    }
  }, []);

  const handleDriverTypeChange = useCallback((value: string) => {
    setDriverType(value);
    setDriverValues(getDriverFormModule(value)?.emptyValues() ?? {});
  }, []);

  const handleDriverValueChange = useCallback((field: string, value: string) => {
    setDriverValues((current) => ({ ...current, [field]: value }));
  }, []);

  const handleAdd = useCallback(() => {
    if (!isValid || !driverFormModule) return;
    onSuccess({
      name: name.trim(),
      siteName: site.trim(),
      buildingName: building.trim(),
      rackName: rack.trim(),
      deviceKind,
      fanCount: fanCountNumber,
      driverType,
      driverConfig: driverFormModule.encode(driverValues),
    });
  }, [
    building,
    deviceKind,
    driverFormModule,
    driverType,
    driverValues,
    fanCountNumber,
    isValid,
    name,
    onSuccess,
    rack,
    site,
  ]);

  useEffect(() => {
    onStateChange({ canAdd: isValid, addHandler: handleAdd });
  }, [handleAdd, isValid, onStateChange]);

  return (
    <div className="flex flex-col gap-4 pb-2">
      <Input id="manual-name" label="Name" onChange={(v) => setName(v)} />
      <InfraLocationFields
        site={site}
        building={building}
        rack={rack}
        siteOptions={siteOptions}
        buildingOptions={buildingOptions}
        rackOptions={rackOptions}
        onSiteChange={setSite}
        onBuildingChange={setBuilding}
        onRackChange={setRack}
      />
      <div className="grid grid-cols-2 gap-3">
        <Select
          id="manual-device-kind"
          label="Target type"
          options={deviceKindOptions}
          value={deviceKind}
          onChange={handleDeviceKindChange}
          forceBelow
        />
        <Input
          id="manual-fan-count"
          label="Fans"
          type="number"
          inputMode="numeric"
          initValue={fanCount}
          readOnly={deviceKind === "single_fan"}
          onChange={(v) => setFanCount(v)}
        />
      </div>
      <Select
        id="manual-driver-type"
        label="Connection type"
        options={driverTypeOptions}
        value={driverType}
        onChange={handleDriverTypeChange}
        forceBelow
      />
      {driverFormModule ? (
        <driverFormModule.FormFields idPrefix="manual" values={driverValues} onChange={handleDriverValueChange} />
      ) : null}
    </div>
  );
};

export default ManualAddStep;
