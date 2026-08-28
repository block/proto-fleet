// Additional types for API responses that are not included in the generated MDK v1 client
// (src/protoOS/api/adapters/mdkV1/generated/).
interface ErrorDetails {
  code?: string;
  message?: string;
}

interface ResponseErrorBody extends ErrorDetails {
  error?: ErrorDetails;
}

interface ResponseErrorProps {
  error?: ResponseErrorBody;
  status: number;
}

export type ErrorProps = ResponseErrorProps | undefined;
