import { useCallback, useEffect, useState } from "react";

import InfraLocationFields from "@/protoFleet/features/infrastructure/components/InfraLocationFields";
import {
  MODBUS_TCP_CONNECTION_TYPE,
  MODBUS_TCP_CONNECTION_TYPE_LABEL,
} from "@/protoFleet/features/infrastructure/connectionTypes";
import { FieldHelpPopover } from "@/protoFleet/features/infrastructure/fieldHelp";
import { infraDeviceFieldHelp } from "@/protoFleet/features/infrastructure/fieldHelpContent";
import type {
  InfraBuildingOption,
  InfraDeviceDraft,
  InfraDeviceEndpointKind,
} from "@/protoFleet/features/infrastructure/types";
import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";

const endpointKindOptions: { value: InfraDeviceEndpointKind; label: string }[] = [
  { value: "single_fan", label: "Single fan" },
  { value: "fan_group", label: "Fan group" },
];
const MIN_MODBUS_UNIT_ID = 1;
const MAX_MODBUS_UNIT_ID = 247;

export interface ManualAddStepState {
  canAdd: boolean;
  addHandler: () => void;
}

interface ManualAddStepProps {
  siteOptions?: string[];
  buildingOptions?: InfraBuildingOption[];
  onSuccess: (device: InfraDeviceDraft) => void;
  onStateChange: (state: ManualAddStepState) => void;
}

const ManualAddStep = ({ siteOptions = [], buildingOptions = [], onSuccess, onStateChange }: ManualAddStepProps) => {
  const [name, setName] = useState("");
  const [unitId, setUnitId] = useState("");
  const [site, setSite] = useState("");
  const [building, setBuilding] = useState("");
  const [endpointKind, setEndpointKind] = useState<InfraDeviceEndpointKind>("single_fan");
  const [fanCount, setFanCount] = useState("1");
  const [endpoint, setEndpoint] = useState("");
  const [port, setPort] = useState("");

  const unitIdValue = unitId.trim();
  const unitIdNumber = Number(unitIdValue);
  const portNumber = Number(port);
  const fanCountNumber = endpointKind === "single_fan" ? 1 : Number(fanCount);
  const isUnitIdValid =
    /^\d+$/.test(unitIdValue) &&
    Number.isSafeInteger(unitIdNumber) &&
    unitIdNumber >= MIN_MODBUS_UNIT_ID &&
    unitIdNumber <= MAX_MODBUS_UNIT_ID;
  const isPortValid = Number.isInteger(portNumber) && portNumber > 0 && portNumber <= 65535;
  const isFanCountValid = endpointKind === "single_fan" || (Number.isInteger(fanCountNumber) && fanCountNumber > 1);
  const isValid =
    [name, unitIdValue, site, building, endpoint].every((value) => value.trim().length > 0) &&
    isUnitIdValid &&
    isPortValid &&
    isFanCountValid;

  const handleEndpointKindChange = useCallback((value: string) => {
    const nextEndpointKind = value as InfraDeviceEndpointKind;
    setEndpointKind(nextEndpointKind);
    if (nextEndpointKind === "single_fan") {
      setFanCount("1");
    }
  }, []);

  const handleAdd = useCallback(() => {
    if (!isValid) return;
    onSuccess({
      unitId: unitIdNumber,
      name: name.trim(),
      siteName: site.trim(),
      buildingName: building.trim(),
      endpointKind,
      fanCount: fanCountNumber,
      connectionType: MODBUS_TCP_CONNECTION_TYPE,
      endpoint: endpoint.trim(),
      port: portNumber,
    });
  }, [building, endpoint, endpointKind, fanCountNumber, isValid, name, onSuccess, portNumber, site, unitIdNumber]);

  useEffect(() => {
    onStateChange({ canAdd: isValid, addHandler: handleAdd });
  }, [handleAdd, isValid, onStateChange]);

  return (
    <div className="flex flex-col gap-4 pb-2">
      <Input id="manual-name" label="Name" onChange={(v) => setName(v)} />
      <InfraLocationFields
        site={site}
        building={building}
        siteOptions={siteOptions}
        buildingOptions={buildingOptions}
        onSiteChange={setSite}
        onBuildingChange={setBuilding}
        allowCustomValues
      />
      <div className="grid grid-cols-2 gap-3">
        <Select
          id="manual-endpoint-kind"
          label="Target type"
          options={endpointKindOptions}
          value={endpointKind}
          onChange={handleEndpointKindChange}
          forceBelow
        />
        <Input
          id="manual-fan-count"
          label="Fans"
          type="number"
          inputMode="numeric"
          initValue={fanCount}
          readOnly={endpointKind === "single_fan"}
          onChange={(v) => setFanCount(v)}
        />
      </div>
      <Input
        id="manual-unit-id"
        label="Unit ID"
        type="number"
        inputMode="numeric"
        suffixAction={<FieldHelpPopover {...infraDeviceFieldHelp.unitId} />}
        onChange={(v) => setUnitId(v)}
      />
      <Input
        id="manual-connection-type"
        label="Connection type"
        initValue={MODBUS_TCP_CONNECTION_TYPE_LABEL}
        readOnly
      />
      <div className="grid grid-cols-2 gap-3">
        <Input
          id="manual-endpoint"
          label="Endpoint"
          suffixAction={<FieldHelpPopover {...infraDeviceFieldHelp.endpoint} />}
          onChange={(v) => setEndpoint(v)}
        />
        <Input
          id="manual-port"
          label="Port"
          type="number"
          inputMode="numeric"
          suffixAction={<FieldHelpPopover {...infraDeviceFieldHelp.port} />}
          onChange={(v) => setPort(v)}
        />
      </div>
    </div>
  );
};

export default ManualAddStep;
