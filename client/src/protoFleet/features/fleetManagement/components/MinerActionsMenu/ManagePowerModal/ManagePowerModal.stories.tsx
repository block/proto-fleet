import { useState } from "react";
import { action } from "storybook/actions";
import ManagePowerModal from "./ManagePowerModal";

export default {
  title: "Proto Fleet/Fleet Management/ManagePowerModal",
  component: ManagePowerModal,
};

// Story wrapper to handle modal visibility
const StoryWrapper = ({ infoMessage }: { infoMessage?: string }) => {
  const [show, setShow] = useState(true);

  if (!show) {
    return (
      <div className="flex h-screen items-center justify-center">
        <button onClick={() => setShow(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
          Show Modal
        </button>
      </div>
    );
  }

  return (
    <div>
      {infoMessage && (
        <div className="mb-4 rounded-lg bg-intent-info-10 p-4 text-300 text-text-primary">{infoMessage}</div>
      )}
      <ManagePowerModal
        onConfirm={(performanceMode) => {
          action("onConfirm")(performanceMode);
          setShow(false);
        }}
        onDismiss={() => {
          action("onDismiss")();
          setShow(false);
        }}
      />
    </div>
  );
};

// Default story
export const Default = () => (
  <StoryWrapper infoMessage="Modal opens with no pre-selected option. Select a power mode and click Confirm to see the selected mode logged in the Actions panel." />
);

// Maximize power option explanation
export const MaximizePower = () => (
  <StoryWrapper infoMessage="The 'Maximize power' option pushes miners to peak hashrate output. Select it and click Confirm to apply." />
);

// Reduce power option explanation
export const ReducePower = () => (
  <StoryWrapper infoMessage="The 'Reduce power' option limits miners to conserve energy and lower costs. Select it and click Confirm to apply." />
);

// Testing dismiss behavior
export const DismissBehavior = () => (
  <StoryWrapper infoMessage="Click outside the modal or press ESC to dismiss. The onDismiss action will be logged." />
);

// Testing confirm behavior
export const ConfirmBehavior = () => (
  <StoryWrapper infoMessage="Select an option and click Confirm. The selected performance mode will be logged in the Actions panel." />
);
