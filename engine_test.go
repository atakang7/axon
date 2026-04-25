package engine

import (
	"testing"
)

func TestOrderBook_Matching(t *testing.T) {
	ob := NewOrderBook()

	// Setup: Add some resting asks
	ob.Process(&Order{ID: "ask1", Price: 100, Quantity: 10, Side: Sell})
	ob.Process(&Order{ID: "ask2", Price: 101, Quantity: 10, Side: Sell})

	// Execute: Buy order that matches ask1 and part of ask2
	trades, err := ob.Process(&Order{ID: "buy1", Price: 102, Quantity: 15, Side: Buy})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(trades) != 2 {
		t.Errorf("Expected 2 trades, got %d", len(trades))
	}

	if trades[0].Price != 100 || trades[0].Quantity != 10 {
		t.Errorf("Trade 1 mismatch: %+v", trades[0])
	}

	if trades[1].Price != 101 || trades[1].Quantity != 5 {
		t.Errorf("Trade 2 mismatch: %+v", trades[1])
	}
}
