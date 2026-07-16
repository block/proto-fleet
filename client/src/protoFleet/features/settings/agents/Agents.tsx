import LLMProviderSettings from "@/protoFleet/features/settings/agents/LLMProviderSettings";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";

const Agents = () => (
  <div className="flex flex-col gap-8">
    <SettingsPageHeader title="Agents" description="Configure the agent harness and model provider used by Proto AI." />
    <LLMProviderSettings />
  </div>
);

export default Agents;
