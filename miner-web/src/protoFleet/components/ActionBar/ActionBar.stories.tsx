import ActionBarComponent from ".";
import { Toaster as ToasterComponent } from "@/shared/features/toaster";

interface ActionBarArgs {
  numberOfMiners: number;
}

export const ActionBar = ({ numberOfMiners }: ActionBarArgs) => {
  return (
    <div>
      <div className="fixed right-4 bottom-4 z-30">
        <ToasterComponent />
      </div>
      <ActionBarComponent selectedMiners={Array(numberOfMiners).fill("MAC")} />
    </div>
  );
};

export default {
  title: "Components (ProtoFleet)/Action Bar",
  args: {
    numberOfMiners: 1,
  },
  argTypes: {
    numberOfMiners: {
      control: { type: "range", min: 1, max: 25, step: 1 },
    },
  },
};
