import { formatControllerTime } from "./coverage";
import Button, { sizes, variants } from "@/shared/components/Button";
import Select from "@/shared/components/Select";

interface ControllerSelectProps {
  id: string;
  controllerIds: string[];
  value: string;
  onChange: (controllerId: string) => void;
}

/** Picks one HA controller; shown only when the source reports more than one. */
export const ControllerSelect = ({ id, controllerIds, value, onChange }: ControllerSelectProps) =>
  controllerIds.length > 1 ? (
    <Select
      id={id}
      label="Controller"
      variant="filter"
      value={value}
      options={[
        { value: "", label: "Preferred controller" },
        ...controllerIds.map((controllerId) => ({ value: controllerId, label: controllerId })),
      ]}
      onChange={onChange}
    />
  ) : null;

interface RefreshControlProps {
  fetchedAt?: string;
  /** The controller that answered the read on screen. */
  controllerId?: string;
  reading: boolean;
  onRefresh: () => void;
  testId: string;
}

/** Reads on demand: when and from which controller the records on screen were read, and a button to read again. */
export const RefreshControl = ({ fetchedAt, controllerId, reading, onRefresh, testId }: RefreshControlProps) => (
  <div className="flex items-center gap-3">
    {fetchedAt ? (
      <span className="text-200 text-text-primary-50" data-testid={`${testId}-read-at`}>
        Read {controllerId ? `from ${controllerId} ` : ""}at {formatControllerTime(fetchedAt)}
      </span>
    ) : null}
    <Button
      size={sizes.compact}
      variant={variants.secondary}
      text="Refresh"
      loading={reading}
      onClick={onRefresh}
      testId={testId}
    />
  </div>
);
