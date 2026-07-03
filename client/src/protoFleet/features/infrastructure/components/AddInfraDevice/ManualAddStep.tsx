import { useCallback, useEffect, useState } from "react";

import InfraLocationFields from "@/protoFleet/features/infrastructure/components/InfraLocationFields";
import { infraDeviceConnectionTypeOptions } from "@/protoFleet/features/infrastructure/connectionTypes";
import { FieldHelpPopover } from "@/protoFleet/features/infrastructure/fieldHelp";
import { infraDeviceFieldHelp } from "@/protoFleet/features/infrastructure/fieldHelpContent";
import type {
  InfraBuildingOption,
  InfraDeviceConnectionType,
  InfraDeviceDraft,
  InfraDeviceEndpointKind,
} from "@/protoFleet/features/infrastructure/types";
import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";

const endpointKindOptions: { value: InfraDeviceEndpointKind; label: string }[] = [
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
  onSuccess: (device: InfraDeviceDraft) => void;
  onStateChange: (state: ManualAddStepState) => void;
}

const ManualAddStep = ({ siteOptions = [], buildingOptions = [], onSuccess, onStateChange }: ManualAddStepProps) => {
  const [name, setName] = useState("");
  const [deviceIdentifier, setDeviceIdentifier] = useState("");
  const [site, setSite] = useState("");
  const [building, setBuilding] = useState("");
  const [endpointKind, setEndpointKind] = useState<InfraDeviceEndpointKind>("single_fan");
  const [fanCount, setFanCount] = useState("1");
  const [connectionType, setConnectionType] = useState<InfraDeviceConnectionType | "">("");
  const [endpoint, setEndpoint] = useState("");
  const [port, setPort] = useState("");

  const portNumber = Number(port);
  const fanCountNumber = endpointKind === "single_fan" ? 1 : Number(fanCount);
  const isPortValid = Number.isInteger(portNumber) && portNumber > 0 && portNumber <= 65535;
  const isFanCountValid = endpointKind === "single_fan" || (Number.isInteger(fanCountNumber) && fanCountNumber > 1);
  const isValid =
    [name, deviceIdentifier, site, building, connectionType, endpoint].every((value) => value.trim().length > 0) &&
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
    if (!isValid || !connectionType) return;
    onSuccess({
      id: deviceIdentifier.trim(),
      name: name.trim(),
      siteName: site.trim(),
      buildingName: building.trim(),
      endpointKind,
      fanCount: fanCountNumber,
      connectionType,
      endpoint: endpoint.trim(),
      port: portNumber,
    });
  }, [
    building,
    connectionType,
    endpoint,
    endpointKind,
    fanCountNumber,
    deviceIdentifier,
    isValid,
    name,
    onSuccess,
    portNumber,
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
        siteOptions={siteOptions}
        buildingOptions={buildingOptions}
        onSiteChange={setSite}
        onBuildingChange={setBuilding}
      />
      <div className="grid grid-cols-2 gap-3">
        <Select
          id="manual-endpoint-kind"
          label="Device type"
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
        id="manual-device-identifier"
        label="Device identifier"
        suffixAction={<FieldHelpPopover {...infraDeviceFieldHelp.deviceIdentifier} />}
        onChange={(v) => setDeviceIdentifier(v)}
      />
      <Select
        id="manual-connection-type"
        label="Connection type"
        options={infraDeviceConnectionTypeOptions}
        value={connectionType}
        onChange={(value) => setConnectionType(value as InfraDeviceConnectionType)}
        suffixAction={<FieldHelpPopover {...infraDeviceFieldHelp.connectionType} />}
        forceBelow
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
