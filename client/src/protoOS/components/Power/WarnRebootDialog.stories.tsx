import { action } from "storybook/actions";
import WarnRebootDialogComponent from "./WarnRebootDialog";

export const WarnRebootDialog = () => {
  return <WarnRebootDialogComponent onClose={action("close dialog")} onSubmit={action("submit dialog")} />;
};

export default {
  title: "Proto OS/Power/WarnRebootDialog",
};
