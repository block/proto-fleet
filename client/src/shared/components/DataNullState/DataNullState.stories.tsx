import { action } from "storybook/actions";
import DataNullStateComponent from "./DataNullState";

export const DataNullState = () => {
  return (
    <DataNullStateComponent
      title="No Data Available"
      description="Test your connection and try again. If the problem persists, contact support or check your network settings."
      onRetry={action("onRetry")}
    />
  );
};

export default {
  title: "Shared/DataNullState",
};
