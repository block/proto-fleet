import { useState } from "react";
import { action } from "storybook/actions";
import CoolingModeModal from "./CoolingModeModal";

export default {
  title: "Proto Fleet/Fleet Management/CoolingModeModal",
  component: CoolingModeModal,
};

// Story wrapper to handle modal visibility
const StoryWrapper = ({ infoMessage, minerCount = 1 }: { infoMessage?: string; minerCount?: number }) => {
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
      <CoolingModeModal
        minerCount={minerCount}
        onConfirm={(coolingMode) => {
          action("onConfirm")(coolingMode);
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

// Default story - single miner
export const Default = () => (
  <StoryWrapper infoMessage="Modal opens with no pre-selected option. Select a cooling mode and click 'Update cooling mode' to see the selected mode logged in the Actions panel." />
);

// Multiple miners
export const MultipleMiners = () => (
  <StoryWrapper
    minerCount={5}
    infoMessage="Modal showing bulk action for 5 miners. Note the pluralized text 'miners' instead of 'miner'."
  />
);

// Air cooled option explanation
export const AirCooled = () => (
  <StoryWrapper infoMessage="The 'Air cooled' option uses fans to cool the miner. Select it and click 'Update cooling mode' to apply." />
);

// Immersion cooled option explanation
export const ImmersionCooled = () => (
  <StoryWrapper infoMessage="The 'Immersion cooled' option disables fans for miners submerged in cooling liquid. Select it and click 'Update cooling mode' to apply." />
);

// Testing dismiss behavior
export const DismissBehavior = () => (
  <StoryWrapper infoMessage="Click 'Done' without selecting an option, click outside the modal, or press ESC to dismiss. The onDismiss action will be logged." />
);
