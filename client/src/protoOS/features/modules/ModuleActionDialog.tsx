import type { ComponentProps } from "react";
import { moduleActions } from "./moduleActions";
import type { useModuleActions } from "./useModuleActions";
import Modal from "@/shared/components/Modal";

const ModuleActionDialog = ({ controls }: { controls: ReturnType<typeof useModuleActions> }) => {
  const { request, pending, outcome, confirm, dismiss } = controls;
  if (!request) return null;
  const action = moduleActions.find((item) => item.id === request.action)!;
  const { target } = request;
  const buttons: ComponentProps<typeof Modal>["buttons"] = outcome
    ? [{ text: "Done", variant: "primary", onClick: dismiss, disabled: pending }]
    : [
        { text: "Cancel", variant: "secondary", onClick: dismiss, disabled: pending },
        {
          text: pending ? "Sending…" : action.label,
          variant: "primary",
          onClick: () => void confirm(),
          disabled: pending,
          dismissModalOnClick: false,
        },
      ];
  return (
    <Modal
      title={action.label}
      dialogLabel={action.label}
      manageFocus
      onDismiss={dismiss}
      buttons={buttons}
      phoneFooterButtons={buttons}
    >
      <div className="space-y-4 text-300">
        <p>{action.description}</p>
        <p>
          {target.identifiers.length} hashboards in {target.names.length}{" "}
          {target.names.length === 1 ? "module" : "modules"}:
        </p>
        <p className="max-h-32 overflow-y-auto text-text-primary-70">{target.names.join(", ")}</p>
        {outcome?.error ? (
          <p role="alert" className="text-text-critical">
            {outcome.error}
          </p>
        ) : null}
        {outcome?.result ? (
          <div role="status" className="space-y-2">
            <p>
              Command accepted for {outcome.result.accepted} of {target.identifiers.length} hashboards.
            </p>
            {outcome.result.accepted < target.identifiers.length ? (
              <p>Some hashboards did not accept the command.</p>
            ) : null}
            {outcome.result.failures.length ? (
              <ul className="max-h-40 overflow-y-auto">
                {outcome.result.failures.map((failure, index) => (
                  <li key={`${failure.identifier}-${index}`}>
                    {failure.identifier}: {failure.message}
                  </li>
                ))}
              </ul>
            ) : null}
            {outcome.refreshFailed ? <p>Could not refresh module status. Waiting for the next update.</p> : null}
          </div>
        ) : null}
      </div>
    </Modal>
  );
};

export default ModuleActionDialog;
