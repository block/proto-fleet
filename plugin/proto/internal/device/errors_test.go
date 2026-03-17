package device

import (
	"testing"
	"time"

	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_common_api"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_data_api"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_error_code"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_fan_api"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_hb_api"
	"github.com/proto-at-block/proto-fleet/server/generated/miner-api/miner_psu_api"
	sdkerrors "github.com/proto-at-block/proto-fleet/server/sdk/v1/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type errorValidationType int

const (
	validateSummaries errorValidationType = iota
	validateFullError
)

const (
	testTimestamp1 = 1234567890 // 2009-02-13 23:31:30 UTC
	testTimestamp2 = 1234567891
	testTimestamp3 = 1234567892

	historicalErrorTimestamp = 1609459200 // 2021-01-01 00:00:00 UTC
)

func TestDevice_ConvertErrorsResponse(t *testing.T) {
	tests := []struct {
		name              string
		response          *miner_data_api.ErrorsResponse
		expectedCount     int
		validationType    errorValidationType
		expectedSummaries []string
		expectedFullError map[string]any
	}{
		{
			name: "PSU output overvoltage includes slot number",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_PsuError{
								PsuError: &miner_psu_api.PsuError{
									Code:  miner_psu_api.PsuErrorCode_PSU_ERROR_CODE_OUTPUT_OVER_VOLTAGE,
									Index: 2,
								},
							},
						},
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Power supply 2 output voltage is too high"},
		},
		{
			name: "fan slow spin with RPM details",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_FanError{
								FanError: &miner_fan_api.FanError{
									Code:  miner_fan_api.FanErrorCode_FAN_ERROR_CODE_SLOW_SPIN,
									Index: 5,
									Detail: &miner_fan_api.FanError_FanSpeed_{
										FanSpeed: &miner_fan_api.FanError_FanSpeed{
											FanPwmTargetPct: 5000,
											FanRpmTach:      1200,
										},
									},
								},
							},
						},
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Fan 5 has stalled. Target RPM: 5000, Actual RPM: 1200"},
		},
		{
			name: "fan slow spin without details falls back gracefully",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_FanError{
								FanError: &miner_fan_api.FanError{
									Code:   miner_fan_api.FanErrorCode_FAN_ERROR_CODE_SLOW_SPIN,
									Index:  3,
									Detail: nil,
								},
							},
						},
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Fan 3 has stalled"},
		},
		{
			name: "rig pool connection failure with URL",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_RigError{
								RigError: &miner_error_code.RigError{
									Code: miner_error_code.RigErrorCode_RIG_ERROR_CODE_POOL_CONNECTION_FAILURE,
									Detail: &miner_error_code.RigError_PoolInfo_{
										PoolInfo: &miner_error_code.RigError_PoolInfo{
											Url: "stratum+tcp://pool.example.com:3333",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Control board is unable to connect to pool stratum+tcp://pool.example.com:3333"},
		},
		{
			name: "rig insufficient cooling with bay index",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_RigError{
								RigError: &miner_error_code.RigError{
									Code: miner_error_code.RigErrorCode_RIG_ERROR_CODE_INSUFFICIENT_COOLING,
									Detail: &miner_error_code.RigError_BayIndex_{
										BayIndex: &miner_error_code.RigError_BayIndex{
											BayIndex: 2,
										},
									},
								},
							},
						},
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Bay 2 has insufficient cooling"},
		},
		{
			name: "rig insufficient cooling without bay index",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_RigError{
								RigError: &miner_error_code.RigError{
									Code:   miner_error_code.RigErrorCode_RIG_ERROR_CODE_INSUFFICIENT_COOLING,
									Detail: nil,
								},
							},
						},
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Bay has insufficient cooling"},
		},
		{
			name: "hashboard ASIC overheat with temperature and ASIC index",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_HbError{
								HbError: &miner_hb_api.HbError{
									Code:      miner_hb_api.HbErrorCode_HB_ERROR_CODE_ASIC_OVER_HEAT,
									Index:     4,
									AsicIndex: &[]uint32{12}[0],
									Detail: &miner_hb_api.HbError_Temperature{
										Temperature: 95.3,
									},
								},
							},
						},
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Hashboard 4 ASIC is overheating: 95.3 °C, first detected at ASIC 13"},
		},
		{
			name: "hashboard board overheat with temperature",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_HbError{
								HbError: &miner_hb_api.HbError{
									Code:  miner_hb_api.HbErrorCode_HB_ERROR_CODE_OVER_HEAT,
									Index: 2,
									Detail: &miner_hb_api.HbError_Temperature{
										Temperature: 88.5,
									},
								},
							},
						},
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Hashboard 2 overheating: 88.5 °C"},
		},
		{
			name: "hashboard overcurrent with amperage",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_HbError{
								HbError: &miner_hb_api.HbError{
									Code:  miner_hb_api.HbErrorCode_HB_ERROR_CODE_OVER_CURRENT,
									Index: 1,
									Detail: &miner_hb_api.HbError_Current{
										Current: 42.5,
									},
								},
							},
						},
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Hashboard 1 overcurrent detected: 42.50 A"},
		},
		{
			name: "hashboard communication errors map to same message",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_HbError{
								HbError: &miner_hb_api.HbError{
									Code:  miner_hb_api.HbErrorCode_HB_ERROR_CODE_COMMUNICATION,
									Index: 2,
								},
							},
						},
					},
					{
						Timestamp: testTimestamp2,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_HbError{
								HbError: &miner_hb_api.HbError{
									Code:  miner_hb_api.HbErrorCode_HB_ERROR_CODE_COMMAND_TIMEOUT,
									Index: 3,
								},
							},
						},
					},
				},
			},
			expectedCount:  2,
			validationType: validateSummaries,
			expectedSummaries: []string{
				"Hashboard 2 communication error",
				"Hashboard 3 communication error",
			},
		},
		{
			name:          "nil response returns empty errors",
			response:      nil,
			expectedCount: 0,
		},
		{
			name: "empty errors array returns empty errors",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{},
			},
			expectedCount: 0,
		},
		{
			name: "error with nil Error field is skipped",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error:     nil,
					},
				},
			},
			expectedCount: 0,
		},
		{
			name: "multiple errors of different types",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_PsuError{
								PsuError: &miner_psu_api.PsuError{
									Code:  miner_psu_api.PsuErrorCode_PSU_ERROR_CODE_OUTPUT_OVER_VOLTAGE,
									Index: 1,
								},
							},
						},
					},
					{
						Timestamp: testTimestamp2,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_FanError{
								FanError: &miner_fan_api.FanError{
									Code:  miner_fan_api.FanErrorCode_FAN_ERROR_CODE_HARDWARE,
									Index: 2,
								},
							},
						},
					},
					{
						Timestamp: testTimestamp3,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_HbError{
								HbError: &miner_hb_api.HbError{
									Code:  miner_hb_api.HbErrorCode_HB_ERROR_CODE_POWER_LOST,
									Index: 3,
								},
							},
						},
					},
				},
			},
			expectedCount:  3,
			validationType: validateSummaries,
			expectedSummaries: []string{
				"Power supply 1 output voltage is too high",
				"Fan 2 hardware error",
				"Hashboard 3 has lost power",
			},
		},
		{
			name: "PSU error maps to correct severity and cause",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_PsuError{
								PsuError: &miner_psu_api.PsuError{
									Code:  miner_psu_api.PsuErrorCode_PSU_ERROR_CODE_NO_INPUT_VOLTAGE,
									Index: 2,
								},
							},
						},
					},
				},
			},
			expectedCount:  1,
			validationType: validateFullError,
			expectedFullError: map[string]any{
				"summary":      "Power supply 2 is not detecting input voltage",
				"minerError":   sdkerrors.PSUInputVoltageLow,
				"severity":     sdkerrors.SeverityCritical,
				"causeSummary": "Loose power cables",
			},
		},
		{
			name: "PSU communication error",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_PsuError{
								PsuError: &miner_psu_api.PsuError{
									Code:  miner_psu_api.PsuErrorCode_PSU_ERROR_CODE_COMM_LOST,
									Index: 1,
								},
							},
						},
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Power supply 1 communication error"},
		},
		{
			name: "PSU overtemperature",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_PsuError{
								PsuError: &miner_psu_api.PsuError{
									Code:  miner_psu_api.PsuErrorCode_PSU_ERROR_CODE_OVER_TEMPERATURE,
									Index: 3,
								},
							},
						},
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Power supply 3 overheating"},
		},
		{
			name: "PSU input undervoltage",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_PsuError{
								PsuError: &miner_psu_api.PsuError{
									Code:  miner_psu_api.PsuErrorCode_PSU_ERROR_CODE_INPUT_UNDER_VOLTAGE,
									Index: 1,
								},
							},
						},
					},
				},
			},
			expectedCount:  1,
			validationType: validateFullError,
			expectedFullError: map[string]any{
				"summary":      "Power supply 1 input voltage is too low",
				"minerError":   sdkerrors.PSUInputVoltageLow,
				"severity":     sdkerrors.SeverityCritical,
				"causeSummary": "Input undervoltage",
			},
		},
		{
			name: "PSU input overvoltage",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_PsuError{
								PsuError: &miner_psu_api.PsuError{
									Code:  miner_psu_api.PsuErrorCode_PSU_ERROR_CODE_INPUT_OVER_VOLTAGE,
									Index: 2,
								},
							},
						},
					},
				},
			},
			expectedCount:  1,
			validationType: validateFullError,
			expectedFullError: map[string]any{
				"summary":      "Power supply 2 input voltage is too high",
				"minerError":   sdkerrors.PSUInputVoltageHigh,
				"severity":     sdkerrors.SeverityCritical,
				"causeSummary": "Input overvoltage",
			},
		},
		{
			name: "PSU input overcurrent",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_PsuError{
								PsuError: &miner_psu_api.PsuError{
									Code:  miner_psu_api.PsuErrorCode_PSU_ERROR_CODE_INPUT_OVER_CURRENT,
									Index: 3,
								},
							},
						},
					},
				},
			},
			expectedCount:  1,
			validationType: validateFullError,
			expectedFullError: map[string]any{
				"summary":      "Power supply 3 input current is too high",
				"minerError":   sdkerrors.PSUFaultGeneric,
				"severity":     sdkerrors.SeverityCritical,
				"causeSummary": "Input overcurrent",
			},
		},
		{
			name: "hashboard undervoltage with voltage",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_HbError{
								HbError: &miner_hb_api.HbError{
									Code:  miner_hb_api.HbErrorCode_HB_ERROR_CODE_UNDER_VOLTAGE,
									Index: 1,
									Detail: &miner_hb_api.HbError_Voltage{
										Voltage: 10.5,
									},
								},
							},
						},
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Hashboard 1 undervoltage detected: 10.50 V"},
		},
		{
			name: "hashboard overvoltage with voltage",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_HbError{
								HbError: &miner_hb_api.HbError{
									Code:  miner_hb_api.HbErrorCode_HB_ERROR_CODE_OVER_VOLTAGE,
									Index: 2,
									Detail: &miner_hb_api.HbError_Voltage{
										Voltage: 14.8,
									},
								},
							},
						},
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Hashboard 2 overvoltage detected at 14.80 V"},
		},
		{
			name: "hashboard ASIC not hashing",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_HbError{
								HbError: &miner_hb_api.HbError{
									Code:      miner_hb_api.HbErrorCode_HB_ERROR_CODE_ASIC_NOT_HASHING,
									Index:     3,
									AsicIndex: &[]uint32{8}[0],
								},
							},
						},
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Hashboard 3 ASIC is not hashing, first detected at ASIC 9"},
		},
		{
			name: "rig network error",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_RigError{
								RigError: &miner_error_code.RigError{
									Code: miner_error_code.RigErrorCode_RIG_ERROR_CODE_NETWORK_ERROR,
								},
							},
						},
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Control board is unable to connect to the network"},
		},
		{
			name: "fan hardware error",
			response: &miner_data_api.ErrorsResponse{
				Errors: []*miner_data_api.ErrorFromDb{
					{
						Timestamp: testTimestamp1,
						Error: &miner_error_code.Error{
							Err: &miner_error_code.Error_FanError{
								FanError: &miner_fan_api.FanError{
									Code:  miner_fan_api.FanErrorCode_FAN_ERROR_CODE_HARDWARE,
									Index: 4,
								},
							},
						},
					},
				},
			},
			expectedCount:     1,
			validationType:    validateSummaries,
			expectedSummaries: []string{"Fan 4 hardware error"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			device := &Device{
				id: "test-device-123",
			}

			// Act
			result := device.convertErrorsResponse(tt.response)

			// Assert
			assert.Equal(t, "test-device-123", result.DeviceID)
			assert.Len(t, result.Errors, tt.expectedCount)

			// Validate based on type
			switch tt.validationType {
			case validateSummaries:
				require.Len(t, result.Errors, len(tt.expectedSummaries))
				for i, expectedSummary := range tt.expectedSummaries {
					assert.Equal(t, expectedSummary, result.Errors[i].Summary)
				}

			case validateFullError:
				require.Len(t, result.Errors, 1)
				assert.Equal(t, tt.expectedFullError["summary"], result.Errors[0].Summary)
				assert.Equal(t, tt.expectedFullError["minerError"], result.Errors[0].MinerError)
				assert.Equal(t, tt.expectedFullError["severity"], result.Errors[0].Severity)
				assert.Equal(t, tt.expectedFullError["causeSummary"], result.Errors[0].CauseSummary)
			}
		})
	}
}

func TestConvertErrorsResponse_LastSeenAtIsCurrentTime(t *testing.T) {
	rigErr := &miner_error_code.RigError{
		Code: miner_error_code.RigErrorCode_RIG_ERROR_CODE_LOW_HASH_RATE,
	}
	errorFromDb := &miner_data_api.ErrorFromDb{
		Timestamp: historicalErrorTimestamp, // 2021-01-01 00:00:00 UTC
		Error: &miner_error_code.Error{
			Err: &miner_error_code.Error_RigError{RigError: rigErr},
		},
	}
	response := &miner_data_api.ErrorsResponse{
		Result: miner_common_api.ApiResult_RESULT_SUCCESS,
		Errors: []*miner_data_api.ErrorFromDb{errorFromDb},
	}

	device := &Device{
		id: "test-device-123",
	}

	// Act
	beforeCall := time.Now()
	result := device.convertErrorsResponse(response)
	afterCall := time.Now()

	// Assert
	require.Len(t, result.Errors, 1)
	err := result.Errors[0]

	// Verify FirstSeenAt is set to the miner's timestamp
	expectedFirstSeenAt := time.Unix(historicalErrorTimestamp, 0)
	assert.Equal(t, expectedFirstSeenAt, err.FirstSeenAt, "FirstSeenAt should be the miner's timestamp")

	// Verify LastSeenAt is set to current time
	assert.False(t, err.LastSeenAt.IsZero(), "LastSeenAt should not be zero")
	assert.True(t, err.LastSeenAt.After(beforeCall) || err.LastSeenAt.Equal(beforeCall),
		"LastSeenAt should be at or after the call time")
	assert.True(t, err.LastSeenAt.Before(afterCall) || err.LastSeenAt.Equal(afterCall),
		"LastSeenAt should be at or before the call completion")
}
