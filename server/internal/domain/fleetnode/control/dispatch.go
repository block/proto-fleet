package control

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Sender dispatches one command to a node's ControlStream. *Registry implements it.
type Sender interface {
	Send(ctx context.Context, fleetNodeID int64, cmd *gatewaypb.ControlCommand, scope ReportScope, kind ReportKind, pair *PairMeta) (*Session, error)
}

// RunCommand dispatches cmd, drains result events through onData until the terminal
// ack, and maps the outcome to an error. Shared by discovery and pairing. kind/pair
// are as in Send; noun names the command in errors. onData returns terminal=true to
// stop early. Returns nil on an OK or PARTIAL ack, error otherwise (or onData's).
func RunCommand(ctx context.Context, sender Sender, fleetNodeID int64, cmd *gatewaypb.ControlCommand, scope ReportScope, kind ReportKind, pair *PairMeta, timeout time.Duration, noun string, onData func(CommandEvent) (terminal bool, err error)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	session, err := sender.Send(ctx, fleetNodeID, cmd, scope, kind, pair)
	if err != nil {
		if errors.Is(err, ErrNoActiveStream) {
			return fleeterror.NewFailedPreconditionError("fleet node has no active control stream")
		}
		return err
	}
	defer session.Close()

	handleEvent := func(ev CommandEvent) (terminal bool, err error) {
		if ev.Ack != nil {
			// PARTIAL: results already streamed, so treat it as usable, not a failure.
			if ev.Ack.GetCode() == gatewaypb.AckCode_ACK_CODE_PARTIAL {
				slog.Warn("fleet node command completed partially",
					"fleet_node_id", fleetNodeID, "command", noun, "detail", ev.Ack.GetErrorMessage())
				return true, nil
			}
			// Require the OK code, not just succeeded=true, so an inconsistent ack
			// can't pass a failed command off as success.
			if ev.Ack.GetCode() != gatewaypb.AckCode_ACK_CODE_OK || !ev.Ack.GetSucceeded() {
				return true, AckFailure(ev.Ack, noun)
			}
			return true, nil
		}
		return onData(ev)
	}

	events := session.Events()
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return connect.NewError(connect.CodeDeadlineExceeded, fmt.Errorf("%s command timed out after %s", noun, timeout))
			}
			// Caller cancelled; report it as such, not a server-side Internal failure.
			return fleeterror.NewCanceledError()
		case ev := <-events:
			if terminal, err := handleEvent(ev); terminal {
				return err
			}
		case <-session.Done():
			// Stream died before an ack; drain buffered events first so select
			// randomness doesn't drop a final ack or last batch.
			for {
				select {
				case ev := <-events:
					if terminal, err := handleEvent(ev); terminal {
						return err
					}
				default:
					return fleeterror.NewFailedPreconditionError("fleet node control stream closed before command completed")
				}
			}
		}
	}
}

// AckFailure maps a non-OK terminal ack to an operator-facing error. The structured
// AckCode drives the gRPC code so BUSY, AGENT_INCAPABLE, and BAD_REQUEST stay
// distinguishable; anything else is an opaque Internal failure.
func AckFailure(ack *gatewaypb.ControlAck, noun string) error {
	reason := ack.GetErrorMessage()
	if reason == "" {
		reason = "code " + ack.GetCode().String()
	}
	// if/else (not switch) so the exhaustive linter doesn't demand a case per AckCode.
	code := ack.GetCode()
	if code == gatewaypb.AckCode_ACK_CODE_BAD_REQUEST {
		return fleeterror.NewInvalidArgumentErrorf("fleet node rejected %s command: %s", noun, reason)
	}
	if code == gatewaypb.AckCode_ACK_CODE_BUSY {
		return fleeterror.NewPlainError(
			fmt.Sprintf("fleet node is busy with another command; retry shortly: %s", reason),
			connect.CodeResourceExhausted,
		)
	}
	if code == gatewaypb.AckCode_ACK_CODE_AGENT_INCAPABLE {
		return fleeterror.NewFailedPreconditionErrorf("fleet node cannot service this %s command; try another node: %s", noun, reason)
	}
	if code == gatewaypb.AckCode_ACK_CODE_REPORT_FAILED {
		return fleeterror.NewInternalErrorf("fleet node could not upload all %s results; some may have been applied, re-list to confirm: %s", noun, reason)
	}
	return fleeterror.NewInternalErrorf("fleet node reported %s failure: %s", noun, reason)
}
