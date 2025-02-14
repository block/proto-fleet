import { Link } from "react-router-dom";

// TODO: remove this after we hook up to protoFleet backeend
import fleet from "../../../../fleet.json";
import FleetLayout from "@/protoFleet/components/FleetLayout";

const Miners = () => {
  return (
    <FleetLayout>
      <h2 className="font-size text-heading-200 text-text-primary">Miners</h2>
      <ul>
        {fleet.map((miner, idx) => {
          return (
            <li key={idx}>
              <Link to={miner.ip}>{miner.name}</Link>
            </li>
          );
        })}
      </ul>
    </FleetLayout>
  );
};

export default Miners;
