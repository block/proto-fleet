import { useNavigate } from "react-router-dom";

import Callout, { intents } from "components/Callout";

import { Alert } from "icons";

interface NoPoolsCalloutProps {
  arePoolsConfigured: boolean;
}

const NoPoolsCallout = ({ arePoolsConfigured }: NoPoolsCalloutProps) => {
  const navigate = useNavigate();

  return (
    <div className="mb-10">
      <Callout
        intent={intents.danger}
        prefixIcon={<Alert className="text-intent-critical-text" />}
        subtitle={
          arePoolsConfigured
            ? "This miner has lost connection to all mining pools."
            : "No mining pools configured."
        }
        buttonText={arePoolsConfigured ? "View pool settings" : "Add mining pools"}
        buttonOnClick={() => navigate("/settings")}
      />
    </div>
  );
};

export default NoPoolsCallout;
