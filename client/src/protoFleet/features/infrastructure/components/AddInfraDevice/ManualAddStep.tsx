import { useCallback, useEffect, useMemo, useState } from "react";

import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";

interface ManualAddStepProps {
  onSuccess: () => void;
  onValidChange: (valid: boolean, pairHandler: () => void) => void;
  siteOptions?: string[];
  buildingOptions?: string[];
  buildingOptionsBySite?: Record<string, string[]>;
}

const toSelectOptions = (values: string[]) => values.map((value) => ({ value, label: value }));

const ManualAddStep = ({
  onSuccess,
  onValidChange,
  siteOptions = [],
  buildingOptions = [],
  buildingOptionsBySite = {},
}: ManualAddStepProps) => {
  const [name, setName] = useState("");
  const [site, setSite] = useState("");
  const [building, setBuilding] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [port, setPort] = useState("");

  const availableBuildingOptions = site ? (buildingOptionsBySite[site] ?? buildingOptions) : buildingOptions;
  const siteSelectOptions = useMemo(() => toSelectOptions(siteOptions), [siteOptions]);
  const buildingSelectOptions = useMemo(() => toSelectOptions(availableBuildingOptions), [availableBuildingOptions]);
  const portNumber = Number(port);
  const isPortValid = Number.isInteger(portNumber) && portNumber > 0 && portNumber <= 65535;
  const isValid = [name, site, building, endpoint].every((value) => value.trim().length > 0) && isPortValid;

  const handleSiteChange = useCallback((value: string) => {
    setSite(value);
    setBuilding("");
  }, []);

  const handleAdd = useCallback(() => {
    if (!isValid) return;
    onSuccess();
  }, [isValid, onSuccess]);

  useEffect(() => {
    onValidChange(isValid, handleAdd);
  }, [isValid, handleAdd, onValidChange]);

  return (
    <div className="flex flex-col gap-4 pb-2">
      <Input id="manual-name" label="Name" onChange={(v) => setName(v)} />
      <div className="grid grid-cols-2 gap-3">
        <Select
          id="manual-site"
          label="Site"
          options={siteSelectOptions}
          value={site}
          onChange={handleSiteChange}
          disabled={siteSelectOptions.length === 0}
        />
        <Select
          id="manual-building"
          label="Building"
          options={buildingSelectOptions}
          value={building}
          onChange={setBuilding}
          disabled={buildingSelectOptions.length === 0}
        />
      </div>
      <div className="grid grid-cols-[1fr_160px] gap-3">
        <Input id="manual-endpoint" label="Endpoint" onChange={(v) => setEndpoint(v)} />
        <Input
          id="manual-port"
          label="Port"
          type="number"
          inputMode="numeric"
          onChange={(v) => setPort(v)}
        />
      </div>
    </div>
  );
};

export default ManualAddStep;
