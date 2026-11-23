package main

import pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"

// Explicit allowlist prevents a future or malformed numeric acknowledgement
// from creating unbounded metric series. Transport failure takes precedence.
func nodeOperationResult(ack *pb.ControlAck, sendErr error) string {
	if sendErr != nil {
		return "ack_send_failed"
	}
	if ack == nil {
		return "missing_ack"
	}
	switch ack.GetCode() {
	case pb.AckCode_ACK_CODE_UNSPECIFIED:
		return "unknown_ack"
	case pb.AckCode_ACK_CODE_OK:
		if ack.GetSucceeded() {
			return "success"
		}
		return "failure"
	case pb.AckCode_ACK_CODE_PARTIAL:
		return "partial"
	case pb.AckCode_ACK_CODE_BUSY:
		return "busy"
	case pb.AckCode_ACK_CODE_BAD_REQUEST:
		return "bad_request"
	case pb.AckCode_ACK_CODE_FAILED_PRECONDITION:
		return "failed_precondition"
	case pb.AckCode_ACK_CODE_AGENT_INCAPABLE:
		return "incapable"
	case pb.AckCode_ACK_CODE_UNIMPLEMENTED:
		return "unsupported"
	case pb.AckCode_ACK_CODE_UNAUTHENTICATED:
		return "miner_auth_rejected"
	case pb.AckCode_ACK_CODE_FORBIDDEN:
		return "miner_forbidden"
	case pb.AckCode_ACK_CODE_SCAN_FAILED:
		return "scan_failed"
	case pb.AckCode_ACK_CODE_REPORT_FAILED:
		return "report_failed"
	case pb.AckCode_ACK_CODE_INTERNAL:
		return "internal"
	default:
		return "unknown_ack"
	}
}
