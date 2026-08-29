// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
package main

import "strings"

func gptModelSupportSystem(model string) bool {
	return !strings.HasPrefix(model, "o1-")
}

func gptModelSupportStream(model string) bool {
	return !strings.HasPrefix(model, "o1-")
}

func gptModelSupportMaxTokens(model string, maxTokens int) int {
	if strings.HasPrefix(model, "o1-") {
		return 0
	}
	return maxTokens
}

func gptModelSupportTemperature(model string, temperature float32) float32 {
	if strings.HasPrefix(model, "o1-") {
		return 0.0
	}
	return temperature
}
