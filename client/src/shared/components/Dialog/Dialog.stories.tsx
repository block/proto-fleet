import { action } from "storybook/actions";
import DialogComponent from ".";
import { SettingsSolid } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";

export const Dialog = () => {
  return (
    <DialogComponent
      title="Title"
      subtitle="Description"
      buttons={[
        {
          text: "Secondary",
          variant: variants.secondary,
          onClick: action("Secondary clicked"),
        },
        {
          text: "Primary",
          variant: variants.primary,
          onClick: action("Primary clicked"),
        },
      ]}
    />
  );
};

export const LongButtonDialog = () => {
  return (
    <DialogComponent
      title="Camera unavailable"
      subtitle="Live scanning needs HTTPS or localhost. Take a photo instead."
      buttons={[
        {
          text: "Dismiss",
          variant: variants.secondary,
          onClick: action("Dismiss clicked"),
        },
        {
          text: "Take photo instead",
          variant: variants.primary,
          onClick: action("Take photo clicked"),
        },
      ]}
    />
  );
};

export const ThreeButtonDialog = () => {
  return (
    <DialogComponent
      title="Miner assigned"
      subtitle="Miner-042 was assigned to Slot 1."
      buttons={[
        {
          text: "Dismiss",
          variant: variants.secondary,
          onClick: action("Dismiss clicked"),
        },
        {
          text: "Undo",
          variant: variants.secondary,
          onClick: action("Undo clicked"),
        },
        {
          text: "Scan next slot",
          variant: variants.primary,
          onClick: action("Scan next slot clicked"),
        },
      ]}
    />
  );
};

export const LoadingDialog = () => {
  return <DialogComponent title="Connecting to your mining pool" subtitle="This may take a few seconds" loading />;
};

export const IconDialog = () => {
  return <DialogComponent title="Title" subtitle="Description" icon={<SettingsSolid />} />;
};

export default {
  title: "Shared/Dialog",
};
