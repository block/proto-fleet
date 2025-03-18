import { Network as NetworkComponent } from ".";

export const Network = () => {
  return (
    <div>
      <NetworkComponent
        submit={() => {}}
        networkName="Bathhouse Williamsburg"
        ipRange="192.168.1.0/24"
        gateway="192.168.1.1"
      />
    </div>
  );
};

export default {
  title: "Components (Shared)/Setup/Network",
  args: {},
  argTypes: {},
};
