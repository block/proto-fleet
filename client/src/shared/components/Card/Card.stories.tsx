import CardComponent, { cardType } from ".";
import Row from "@/shared/components/Row";

export const Card = () => {
  return (
    <div className="w-80 space-y-4">
      <CardComponent title="Default" type={cardType.default}>
        <Row>Row</Row>
      </CardComponent>
      <CardComponent title="Success" type={cardType.success}>
        <Row>Row</Row>
      </CardComponent>
      <CardComponent title="Warning" type={cardType.warning}>
        <Row>Row</Row>
      </CardComponent>
    </div>
  );
};

export default {
  title: "Shared/Card",
};
