import Row from "components/Row";

import CardComponent, { cardType } from ".";

export const Card = () => {
  return (
    <div className="space-y-4 w-80">
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
  title: "Components/Card",
};
