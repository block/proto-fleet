import { action } from "storybook/actions";
import { SingleMinerActionsMenu } from ".";

export const Default = () => {
  return (
    <div className="flex h-screen items-center justify-center bg-surface-base">
      <div className="flex w-full max-w-md items-center justify-between rounded-lg border border-border-10 bg-surface-default p-4">
        <span className="text-emphasis-300 text-text-primary">Miner-001</span>
        <SingleMinerActionsMenu
          deviceIdentifier="miner-001"
          onActionStart={action("Action started")}
          onActionComplete={action("Action completed")}
        />
      </div>
    </div>
  );
};

export const InTable = () => {
  return (
    <div className="flex h-screen items-center justify-center bg-surface-base p-8">
      <table className="w-full max-w-4xl border-collapse">
        <thead>
          <tr className="border-b border-border-10">
            <th className="px-4 py-3 text-left text-emphasis-300 text-text-primary">Name</th>
            <th className="px-4 py-3 text-left text-emphasis-300 text-text-primary">Status</th>
            <th className="px-4 py-3 text-left text-emphasis-300 text-text-primary">Hashrate</th>
          </tr>
        </thead>
        <tbody>
          {["Miner-001", "Miner-002", "Miner-003", "Miner-004", "Miner-005"].map((name, index) => (
            <tr key={name} className="border-b border-border-10 hover:bg-surface-elevated-base/50">
              <td className="px-4 py-3">
                <div className="flex w-full items-center justify-between">
                  <span className="text-emphasis-300 text-text-primary">{name}</span>
                  <SingleMinerActionsMenu
                    deviceIdentifier={name.toLowerCase()}
                    onActionStart={action(`${name} - Action started`)}
                    onActionComplete={action(`${name} - Action completed`)}
                  />
                </div>
              </td>
              <td className="px-4 py-3 text-300 text-text-primary-70">{index % 2 === 0 ? "Online" : "Offline"}</td>
              <td className="px-4 py-3 text-300 text-text-primary-70">{100 + index * 10} TH/s</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
};

export const MultipleInList = () => {
  return (
    <div className="flex h-screen items-center justify-center bg-surface-base p-8">
      <div className="w-full max-w-md space-y-2">
        {["Miner-001", "Miner-002", "Miner-003", "Miner-004"].map((name) => (
          <div
            key={name}
            className="flex items-center justify-between rounded-lg border border-border-10 bg-surface-default p-4 hover:bg-surface-elevated-base/50"
          >
            <span className="text-emphasis-300 text-text-primary">{name}</span>
            <SingleMinerActionsMenu
              deviceIdentifier={name.toLowerCase()}
              onActionStart={action(`${name} - Action started`)}
              onActionComplete={action(`${name} - Action completed`)}
            />
          </div>
        ))}
      </div>
    </div>
  );
};

export default {
  title: "Proto Fleet/Miner Actions Menu/Single Miner Actions Menu",
  component: SingleMinerActionsMenu,
};
