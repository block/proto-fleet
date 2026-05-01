package modes

import (
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"

	pb "github.com/block/proto-fleet/server/generated/grpc/curtailment/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type FixedKW struct {
	params *pb.FixedKwParams
}

func NewFixedKW(params *pb.FixedKwParams) *FixedKW {
	return &FixedKW{params: params}
}

func (m *FixedKW) Select(candidates []Candidate) ([]Candidate, error) {
	if len(candidates) == 0 {
		return nil, fleeterror.NewInvalidArgumentError(ErrNoCurtailableCandidates.Error())
	}
	if m.params == nil {
		return nil, fleeterror.NewInvalidArgumentError("fixed_kw params are required")
	}

	targetKW := m.params.GetTargetKw()
	if targetKW <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("fixed_kw.target_kw must be greater than 0")
	}
	toleranceKW := m.params.GetToleranceKw()
	if toleranceKW < 0 {
		return nil, fleeterror.NewInvalidArgumentError("fixed_kw.tolerance_kw must be greater than or equal to 0")
	}

	availableKW := totalKW(candidates)
	if availableKW < targetKW-toleranceKW {
		return nil, insufficientLoadError(availableKW, targetKW, toleranceKW)
	}

	selected := make([]Candidate, 0, len(candidates))
	var realizedKW float64
	for _, candidate := range candidates {
		selected = append(selected, candidate)
		realizedKW += candidate.CurrentPowerW / 1000
		if realizedKW >= targetKW {
			return selected, nil
		}
	}

	return selected, nil
}

func insufficientLoadError(availableKW, targetKW, toleranceKW float64) error {
	err := connect.NewError(
		connect.CodeInvalidArgument,
		fmt.Errorf("insufficient curtailable load: %.3f kW available, %.3f kW requested", availableKW, targetKW),
	)
	detail, detailErr := structpb.NewStruct(map[string]any{
		"available_kw": availableKW,
		"requested_kw": targetKW,
		"tolerance_kw": toleranceKW,
	})
	if detailErr != nil {
		return errors.Join(err, detailErr)
	}
	connectDetail, detailErr := connect.NewErrorDetail(detail)
	if detailErr != nil {
		return errors.Join(err, detailErr)
	}
	err.AddDetail(connectDetail)
	return err
}
