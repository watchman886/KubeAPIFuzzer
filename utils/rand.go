package utils

import (
	"k-fuzz/values"
	"math/rand/v2"

	"github.com/spf13/viper"
)

var R = rand.New(rand.NewPCG(viper.GetUint64(values.Seed), 0))
