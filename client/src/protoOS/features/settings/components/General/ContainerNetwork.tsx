import { useNetworkInfo } from "@/protoOS/api";
import Row from "@/shared/components/Row";

const ContainerNetwork = () => {
  const { data, error } = useNetworkInfo({ poll: false });
  const rows = [
    ["Network connection", data?.dhcp === undefined ? undefined : data.dhcp ? "DHCP (Automatic)" : "Static"],
    ["IP Address", data?.ipAddress],
    ["Subnet Mask", data?.netmask],
    ["Gateway", data?.gateway],
  ];

  return (
    <section className="mb-10" aria-labelledby="container-network-heading">
      <h3 id="container-network-heading" className="mb-2 text-heading-100">
        Network
      </h3>
      {rows.map(([label, value]) => (
        <Row key={label} className="flex justify-between gap-4">
          <h4 className="text-emphasis-300">{label}</h4>
          <div className="text-right text-300 break-all">{error ? "—" : value || "—"}</div>
        </Row>
      ))}
    </section>
  );
};

export default ContainerNetwork;
