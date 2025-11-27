import { Network as NetworkComponent } from ".";

interface NetworkArgs {
  subnet: string;
}

export const Network = ({ subnet }: NetworkArgs) => {
  return (
    <div>
      <NetworkComponent submit={() => {}} subnet={subnet} gateway="192.168.1.1" />
    </div>
  );
};

export default {
  title: "Shared/Setup/Network",
  args: {
    subnet: "192.168.1.0/24",
  },
  argTypes: {
    subnet: {
      control: "select",
      options: ["192.168.1.0/24", "255.255.255.0"],
    },
  },
};
