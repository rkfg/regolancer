package main

import (
	"testing"
)

func TestCalcEconMarginMsat(t *testing.T) {
	tests := []struct {
		name             string
		amtMsat          int64
		feeRateMilliMsat int64
		ratio            float64
		marginPpm        int64
		lostProfitPPM    int64
		expectedResult   int64
	}{
		{
			name:             "no econ ratio margin; ",
			amtMsat:          2_000_000,
			feeRateMilliMsat: 1000,
			ratio:            1,
			marginPpm:        100,
			lostProfitPPM:    200,
			expectedResult:   0,
		},
		{
			name:             "econ ratio set and relevant",
			amtMsat:          2_000_000,
			feeRateMilliMsat: 1000,
			ratio:            0.7, // marginMsat is 30% * 2000msat = 600msat
			marginPpm:        100,
			lostProfitPPM:    200,
			expectedResult:   600,
		},
		{
			name:             "lost profit below margin",
			amtMsat:          2_000_000,
			feeRateMilliMsat: 1000,
			ratio:            0.99,
			marginPpm:        100,
			lostProfitPPM:    80,
			expectedResult:   60, // 20msat from ratio and 40msat from the difference of marginPpm and lostProfitPPM
		},
		{
			name:             "lost profit at zero, but margin ppm",
			amtMsat:          2_000_000,
			feeRateMilliMsat: 1000,
			ratio:            0.99,
			marginPpm:        100,
			lostProfitPPM:    0,
			expectedResult:   220, // 20msat from ratio and 200msat from the difference of marginPpm and lostProfitPPM
		},
		{
			name:             "no lost profit",
			amtMsat:          2_000_000,
			feeRateMilliMsat: 1000,
			ratio:            0.99,
			marginPpm:        0,
			lostProfitPPM:    0,
			expectedResult:   20, // 20msat from ratio
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calcEconMarginMsat(tt.amtMsat, tt.feeRateMilliMsat, tt.ratio, tt.marginPpm, tt.lostProfitPPM)
			if result != tt.expectedResult {
				t.Errorf("name: %v, calcEconMarginMsat() = %v, want %v", tt.name, result, tt.expectedResult)
			}
		})
	}
}
