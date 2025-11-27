import { ReactNode } from "react";
import { Link, useParams } from "react-router-dom";
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";
import { DismissCircleDark } from "@/shared/assets/icons";

const CloseButton = ({ id }: { id: string }) => {
  return (
    <Link className="flex flex-row items-center gap-1 pl-2 text-300 text-text-primary-70" to={"/miners"}>
      <DismissCircleDark />
      {id}
    </Link>
  );
};

const SingleMinerWrapper = ({ children }: { children: ReactNode }) => {
  const { id } = useParams();

  // Here we are just setting the base url to <vite_server>/:id,
  // which vite proxies to the actual miner api server.
  // If we wanted to make this request to ProtoFleet backend we
  // could pass <protofleet_host>/miners/:id instead
  return (
    <MinerHostingProvider
      baseUrl={id || ""}
      minerRoot={`/miners/${id}`}
      closeButton={(<CloseButton id={id || ""} />) as ReactNode}
    >
      {children}
    </MinerHostingProvider>
  );
};

export default SingleMinerWrapper;
