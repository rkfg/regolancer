package main

func absoluteDeltaPPM(base, amt int64) (deltaPPM int64) {

	deltaPPM = (base - amt) * 1e6 / base
	if deltaPPM < 0 {
		return -deltaPPM
	}
	return deltaPPM
}
