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

export const LoadingDialog = () => {
  return <DialogComponent title="Connecting to your mining pool" subtitle="This may take a few seconds" loading />;
};

export const IconDialog = () => {
  return <DialogComponent title="Title" subtitle="Description" icon={<SettingsSolid />} />;
};

export default {
  title: "Shared/Dialog",
};
