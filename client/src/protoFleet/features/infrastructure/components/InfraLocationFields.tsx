import { useCallback, useMemo } from "react";

import type { InfraBuildingOption, InfraRackOption } from "@/protoFleet/features/infrastructure/types";
import Select from "@/shared/components/Select";

const buildOptions = (values: string[], currentValue: string) =>
  [...new Set([currentValue, ...values].filter(Boolean))].sort().map((value) => ({ value, label: value }));

const buildRackOptions = (values: string[], currentValue: string) => {
  const rackOptions = buildOptions(values, currentValue);
  return rackOptions.length === 0 ? [] : [{ value: "", label: "No rack" }, ...rackOptions];
};

const selectCurrentOrFirst = (values: string[], currentValue: string) =>
  values.includes(currentValue) ? currentValue : (values[0] ?? "");

const selectRackForLocation = (values: string[], currentValue: string) =>
  currentValue !== "" && values.includes(currentValue) ? currentValue : "";

const buildingNamesForSite = (options: InfraBuildingOption[], site: string) =>
  options.filter((option) => option.siteName === site).map((option) => option.buildingName);

const rackNamesForLocation = (options: InfraRackOption[], site: string, building: string) =>
  options
    .filter((option) => option.siteName === site && option.buildingName === building)
    .map((option) => option.rackName);

interface InfraLocationFieldsProps {
  site: string;
  building: string;
  rack: string;
  siteOptions: string[];
  buildingOptions: InfraBuildingOption[];
  rackOptions: InfraRackOption[];
  onSiteChange: (site: string) => void;
  onBuildingChange: (building: string) => void;
  onRackChange: (rack: string) => void;
  disabled?: boolean;
}

const InfraLocationFields = ({
  site,
  building,
  rack,
  siteOptions,
  buildingOptions,
  rackOptions,
  onSiteChange,
  onBuildingChange,
  onRackChange,
  disabled = false,
}: InfraLocationFieldsProps) => {
  const siteSelectOptions = useMemo(() => buildOptions(siteOptions, site), [siteOptions, site]);
  const matchingBuildingNames = useMemo(() => buildingNamesForSite(buildingOptions, site), [buildingOptions, site]);
  const buildingSelectOptions = useMemo(
    () => buildOptions(matchingBuildingNames, building),
    [building, matchingBuildingNames],
  );
  const matchingRackNames = useMemo(
    () => rackNamesForLocation(rackOptions, site, building),
    [building, rackOptions, site],
  );
  const rackSelectOptions = useMemo(() => buildRackOptions(matchingRackNames, rack), [matchingRackNames, rack]);

  const handleSiteChange = useCallback(
    (nextSite: string) => {
      onSiteChange(nextSite);

      const nextBuilding = selectCurrentOrFirst(buildingNamesForSite(buildingOptions, nextSite), building);
      if (nextBuilding !== building) {
        onBuildingChange(nextBuilding);
      }

      const nextRack = selectRackForLocation(rackNamesForLocation(rackOptions, nextSite, nextBuilding), rack);
      if (nextRack !== rack) {
        onRackChange(nextRack);
      }
    },
    [building, buildingOptions, onBuildingChange, onRackChange, onSiteChange, rack, rackOptions],
  );

  const handleBuildingChange = useCallback(
    (nextBuilding: string) => {
      onBuildingChange(nextBuilding);
      const nextRack = selectRackForLocation(rackNamesForLocation(rackOptions, site, nextBuilding), rack);
      if (nextRack !== rack) {
        onRackChange(nextRack);
      }
    },
    [onBuildingChange, onRackChange, rack, rackOptions, site],
  );

  return (
    <div className="grid grid-cols-1 gap-3 tablet:grid-cols-3">
      <Select
        id="infra-location-site"
        label="Site"
        options={siteSelectOptions}
        value={site}
        onChange={handleSiteChange}
        disabled={disabled || siteSelectOptions.length === 0}
        forceBelow
      />
      <Select
        id="infra-location-building"
        label="Building"
        options={buildingSelectOptions}
        value={building}
        onChange={handleBuildingChange}
        disabled={disabled || site === "" || buildingSelectOptions.length === 0}
        forceBelow
      />
      <Select
        id="infra-location-rack"
        label="Rack"
        options={rackSelectOptions}
        value={rack}
        onChange={onRackChange}
        disabled={disabled || site === "" || building === "" || rackSelectOptions.length === 0}
        forceBelow
      />
    </div>
  );
};

export default InfraLocationFields;
