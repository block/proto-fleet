// Additional types for API responses that are not included in the generatedApi.ts
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
