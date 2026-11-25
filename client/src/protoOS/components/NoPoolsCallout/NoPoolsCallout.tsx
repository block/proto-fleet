import { navigationItems } from "@/protoOS/components/NavigationMenu/constants";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { Alert } from "@/shared/assets/icons";
import Callout, { intents } from "@/shared/components/Callout";
import { useNavigate } from "@/shared/hooks/useNavigate";

interface NoPoolsCalloutProps {
  arePoolsConfigured: boolean;
}

const NoPoolsCallout = ({ arePoolsConfigured }: NoPoolsCalloutProps) => {
  const navigate = useNavigate();
  const { minerRoot, isFleetHosted } = useMinerHosting();

  return (
    <div className="mb-10">
      <Callout
        intent={intents.danger}
        prefixIcon={<Alert />}
        title={
          arePoolsConfigured ? "This miner has lost connection to all mining pools." : "No mining pools configured."
        }
        // Fleet-hosted pools are read-only in the embedded view, so always frame
        // the CTA as "view" (adding happens through Fleet's pool flow).
        buttonText={arePoolsConfigured || isFleetHosted ? "View pool settings" : "Add mining pools"}
        // Prefix minerRoot so the link stays inside the embedded miner view when
        // fleet-hosted; an absolute path would escape to ProtoFleet's settings.
        buttonOnClick={() => navigate(`${minerRoot}/${navigationItems.miningPools}`)}
      />
    </div>
  );
};

export default NoPoolsCallout;
