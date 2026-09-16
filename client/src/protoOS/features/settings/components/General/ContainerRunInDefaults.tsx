import { useState } from "react";
import { parseDraft, toDraft } from "@/protoOS/features/runIn/defaults";
import RunInForm from "@/protoOS/features/runIn/RunInForm";
import { useRunInDefaults } from "@/protoOS/features/runIn/useRunInDefaults";
import Button from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import Row from "@/shared/components/Row";

const ContainerRunInDefaults = () => {
  const { defaults, save, deviceId } = useRunInDefaults();
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState(() => toDraft(defaults));
  const [error, setError] = useState<string>();

  const saveDefaults = () => {
    let value;
    try {
      value = parseDraft(draft);
    } catch (error) {
      setError((error as Error).message);
      return;
    }
    try {
      save(value);
      setOpen(false);
    } catch {
      setError("Could not save defaults in this browser.");
    }
  };

  const rows = [
    ["Duration", defaults ? `${defaults.durationMinutes} min` : "—"],
    ["Sample interval", defaults ? `${defaults.sampleIntervalSeconds} s` : "—"],
    ["Cooldown", defaults ? `${defaults.cooldownMinutes} min` : "—"],
    ["Power target per hashboard", defaults ? `${defaults.boardPowerTargetWatts} W` : "—"],
    ["Stop mining when finished", defaults ? (defaults.stopMiningOnFinish ? "On" : "Off") : "—"],
  ];
  return (
    <section className="mb-10" aria-label="Run-in defaults">
      <div className="mb-2 flex items-center justify-between">
        <h3 className="text-heading-100">Run-in defaults</h3>
        <Button
          text="Edit"
          ariaLabel="Edit run-in defaults"
          variant="secondary"
          size="compact"
          disabled={!deviceId}
          onClick={() => {
            setDraft(toDraft(defaults));
            setError(undefined);
            setOpen(true);
          }}
        />
      </div>
      {rows.map(([label, value]) => (
        <Row key={label} className="flex justify-between gap-4">
          <h4>{label}</h4>
          <span>{value}</span>
        </Row>
      ))}
      {open ? (
        <Modal
          open
          title="Run-in defaults"
          dialogLabel="Run-in defaults"
          manageFocus
          onDismiss={() => setOpen(false)}
          buttons={[
            {
              text: "Save",
              variant: "primary",
              dismissModalOnClick: false,
              onClick: saveDefaults,
            },
          ]}
        >
          <p className="mb-6 text-text-primary-70">Saved in this browser for this container.</p>
          {error ? (
            <p role="alert" className="mb-4 text-intent-critical-fill">
              {error}
            </p>
          ) : null}
          <RunInForm draft={draft} onChange={setDraft} />
        </Modal>
      ) : null}
    </section>
  );
};

export default ContainerRunInDefaults;
