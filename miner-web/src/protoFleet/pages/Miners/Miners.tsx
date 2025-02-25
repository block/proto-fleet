import { Link } from "react-router-dom";

// TODO: remove this after we hook up to protoFleet backeend
import fleet from "../../../../fleet.json";

const Miners = () => {
  return (
    <>
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
    </>
  );
};

export default Miners;
