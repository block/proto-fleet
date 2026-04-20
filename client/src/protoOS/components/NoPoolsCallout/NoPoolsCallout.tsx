import { Alert } from "@/shared/assets/icons";
import Callout, { intents } from "@/shared/components/Callout";
import { useNavigate } from "@/shared/hooks/useNavigate";

interface NoPoolsCalloutProps {
  arePoolsConfigured: boolean;
}

const NoPoolsCallout = ({ arePoolsConfigured }: NoPoolsCalloutProps) => {
  const navigate = useNavigate();

  return (
    <div className="mb-10">
      <Callout
        intent={intents.danger}
        prefixIcon={<Alert />}
        title={
          arePoolsConfigured ? "This miner has lost connection to all mining pools." : "No mining pools configured."
        }
        buttonText={arePoolsConfigured ? "View pool settings" : "Add mining pools"}
        buttonOnClick={() => navigate("/settings/mining-pools")}
      />
    </div>
  );
};

export default NoPoolsCallout;
