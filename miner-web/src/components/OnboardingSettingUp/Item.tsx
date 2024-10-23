import Row from "components/Row";
import Spinner from "components/Spinner";

import { Alert, Success } from "icons";

import { statuses } from "./constants";
import Error from "./Error";

interface ItemProps {
  divider?: boolean;
  onClickRetry: () => void;
  status: keyof typeof statuses;
  text: string;
}

const Item = ({ divider = true, onClickRetry, status, text }: ItemProps) => {
  return (
    <Row className="flex" divider={divider}>
      <div className="grow">
        <div className="text-emphasis-300">Configuring your {text}</div>
        {status === statuses.error && (
          <div className="text-200 text-text-primary-70">
            <Error text={text} onClickRetry={onClickRetry} />
          </div>
        )}
      </div>
      <div className="ml-4">
        {(status === statuses.fetch || status === statuses.pending) && (
          <Spinner />
        )}
        {status === statuses.success && (
          <Success className="text-intent-success-fill" />
        )}
        {status === statuses.error && (
          <Alert className="text-intent-warning-fill" />
        )}
      </div>
    </Row>
  );
};

export default Item;
