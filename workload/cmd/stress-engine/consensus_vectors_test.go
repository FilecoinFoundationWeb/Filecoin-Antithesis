package main

import (
	"testing"

	"github.com/filecoin-project/go-state-types/abi"
)

func TestF3StallDetection(t *testing.T) {
	tests := []struct {
		name           string
		prev           f3StallState
		minFinH        abi.ChainEpoch
		maxHead        abi.ChainEpoch
		finCount       int
		wantFallback   bool
		wantLastFinH   abi.ChainEpoch
		wantHeadAtAdv  abi.ChainEpoch
	}{
		{
			name:         "initial state — F3 advancing",
			prev:         f3StallState{lastFinalizedH: 0, headAtLastAdvance: 0},
			minFinH:      100,
			maxHead:      110,
			finCount:     2,
			wantFallback: false,
			wantLastFinH: 100,
			wantHeadAtAdv: 110,
		},
		{
			name:         "F3 keeps advancing — no fallback",
			prev:         f3StallState{lastFinalizedH: 100, headAtLastAdvance: 110},
			minFinH:      120,
			maxHead:      130,
			finCount:     2,
			wantFallback: false,
			wantLastFinH: 120,
			wantHeadAtAdv: 130,
		},
		{
			name:         "F3 stalled but within grace period — no fallback yet",
			prev:         f3StallState{lastFinalizedH: 100, headAtLastAdvance: 110},
			minFinH:      100,
			maxHead:      140,
			finCount:     2,
			wantFallback: false,
			wantLastFinH: 100,
			wantHeadAtAdv: 110,
		},
		{
			name:         "F3 stalled past grace period — fallback activates",
			prev:         f3StallState{lastFinalizedH: 100, headAtLastAdvance: 110},
			minFinH:      100,
			maxHead:      161,
			finCount:     2,
			wantFallback: true,
			wantLastFinH: 100,
			wantHeadAtAdv: 110,
		},
		{
			name:         "F3 stalled exactly at grace boundary — fallback activates",
			prev:         f3StallState{lastFinalizedH: 100, headAtLastAdvance: 110},
			minFinH:      100,
			maxHead:      160,
			finCount:     2,
			wantFallback: true,
			wantLastFinH: 100,
			wantHeadAtAdv: 110,
		},
		{
			name:         "fallback already active — stays active while stalled",
			prev:         f3StallState{lastFinalizedH: 100, headAtLastAdvance: 110, fallbackActive: true},
			minFinH:      100,
			maxHead:      200,
			finCount:     2,
			wantFallback: true,
			wantLastFinH: 100,
			wantHeadAtAdv: 110,
		},
		{
			name:         "F3 resumes — fallback deactivates",
			prev:         f3StallState{lastFinalizedH: 100, headAtLastAdvance: 110, fallbackActive: true},
			minFinH:      150,
			maxHead:      160,
			finCount:     2,
			wantFallback: false,
			wantLastFinH: 150,
			wantHeadAtAdv: 160,
		},
		{
			name:         "only 1 node responded — no stall detection",
			prev:         f3StallState{lastFinalizedH: 100, headAtLastAdvance: 110},
			minFinH:      100,
			maxHead:      200,
			finCount:     1,
			wantFallback: false,
			wantLastFinH: 100,
			wantHeadAtAdv: 110,
		},
		{
			name:         "finalized height regresses (node lagging) — treated as stall",
			prev:         f3StallState{lastFinalizedH: 100, headAtLastAdvance: 110},
			minFinH:      90,
			maxHead:      200,
			finCount:     2,
			wantFallback: true,
			wantLastFinH: 100,
			wantHeadAtAdv: 110,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := updateF3StallDetection(tt.prev, tt.minFinH, tt.maxHead, tt.finCount)

			if got.fallbackActive != tt.wantFallback {
				t.Errorf("fallbackActive = %v, want %v", got.fallbackActive, tt.wantFallback)
			}
			if got.lastFinalizedH != tt.wantLastFinH {
				t.Errorf("lastFinalizedH = %d, want %d", got.lastFinalizedH, tt.wantLastFinH)
			}
			if got.headAtLastAdvance != tt.wantHeadAtAdv {
				t.Errorf("headAtLastAdvance = %d, want %d", got.headAtLastAdvance, tt.wantHeadAtAdv)
			}
		})
	}
}

func TestF3StallDetectionLifecycle(t *testing.T) {
	state := f3StallState{}

	// Epoch 0-100: F3 advancing normally
	state = updateF3StallDetection(state, 50, 60, 2)
	if state.fallbackActive {
		t.Fatal("should not be active during normal F3")
	}

	state = updateF3StallDetection(state, 80, 90, 2)
	if state.fallbackActive {
		t.Fatal("should not be active during normal F3")
	}

	// F3 stalls at height 100 (e.g. miner slash broke quorum)
	state = updateF3StallDetection(state, 100, 110, 2)
	stalledState := state

	// Chain keeps growing but F3 is stuck
	state = updateF3StallDetection(state, 100, 120, 2)
	if state.fallbackActive {
		t.Fatal("should not activate within grace period")
	}

	state = updateF3StallDetection(state, 100, 140, 2)
	if state.fallbackActive {
		t.Fatal("should not activate within grace period")
	}

	// Head reaches 110 + 50 = 160 → grace period exceeded
	state = updateF3StallDetection(state, 100, stalledState.headAtLastAdvance+f3StallGraceEpochs, 2)
	if !state.fallbackActive {
		t.Fatal("should activate after grace period")
	}

	// Stays active while stalled
	state = updateF3StallDetection(state, 100, 200, 2)
	if !state.fallbackActive {
		t.Fatal("should stay active while F3 stalled")
	}

	// F3 resumes (e.g. power redistributed, quorum restored)
	state = updateF3StallDetection(state, 150, 160, 2)
	if state.fallbackActive {
		t.Fatal("should deactivate when F3 resumes")
	}
	if state.lastFinalizedH != 150 {
		t.Fatalf("lastFinalizedH = %d, want 150", state.lastFinalizedH)
	}
}
