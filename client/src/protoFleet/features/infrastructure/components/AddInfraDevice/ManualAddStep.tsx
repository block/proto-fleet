import { useCallback, useEffect, useMemo, useState } from "react";

import Input from "@/shared/components/Input";
import Select from "@/shared/components/Select";
import { positions } from "@/shared/constants";

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
  const [identifier, setIdentifier] = useState("");

  const availableBuildingOptions = site ? (buildingOptionsBySite[site] ?? buildingOptions) : buildingOptions;
  const siteSelectOptions = useMemo(() => toSelectOptions(siteOptions), [siteOptions]);
  const buildingSelectOptions = useMemo(() => toSelectOptions(availableBuildingOptions), [availableBuildingOptions]);
  const isValid = [name, site, building, identifier].every((value) => value.trim().length > 0);

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
      <Input
        id="manual-identifier"
        label="Bridge/device identifier"
        onChange={(v) => setIdentifier(v)}
        tooltip={{
          header: "Bridge/device ID",
          body: "Use the stable ID Fleet sends commands to. For a bridge or PLC, include the controller ID and endpoint when needed, like plc-aus-b1-01:fan-bank-a or modbus-den-b1-03:f03. Keep it unique within the site.",
          position: positions["top left"],
          widthClassName: "w-[360px]",
        }}
      />
    </div>
  );
};

export default ManualAddStep;
