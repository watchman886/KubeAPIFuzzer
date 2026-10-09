package utils

import "math"

func Pow(x, y float64) float64 {
	return float64(int64(float64Pow(x, y)))
}

func float64Pow(x, y float64) float64 {
	return exp(y * log(x))
}

func log(x float64) float64 {
	return math.Log(x)
}

func exp(x float64) float64 {
	return math.Exp(x)
}
