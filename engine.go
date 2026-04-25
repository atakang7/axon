package engine

import (
	"container/list"
	"fmt"
	"sync"
	"time"
)

type Side int

const (
	Buy Side = iota
	Sell
)

type Order struct {
	ID        string
	Price     uint64
	Quantity  uint64
	Side      Side
	Timestamp time.Time
}

type OrderBook struct {
	mu   sync.Mutex
	bids *PriceLevelMap
	asks *PriceLevelMap
}

type PriceLevelMap struct {
	levels map[uint64]*list.List // Price -> List of Orders
	sorted []uint64              // Sorted prices
	side   Side
}

func NewPriceLevelMap(side Side) *PriceLevelMap {
	return &PriceLevelMap{
		levels: make(map[uint64]*list.List),
		sorted: make([]uint64, 0),
		side:   side,
	}
}

func (plm *PriceLevelMap) addOrder(order *Order) {
	if l, ok := plm.levels[order.Price]; ok {
		l.PushBack(order)
	} else {
		newList := list.New()
		newList.PushBack(order)
		plm.levels[order.Price] = newList
		plm.insertPrice(order.Price)
	}
}

func (plm *PriceLevelMap) insertPrice(price uint64) {
	// Simple insertion sort for the price slice
	// In a production system, a Red-Black tree or B-Tree is preferred
	for i, p := range plm.sorted {
		if plm.side == Buy {
			if price > p {
				plm.sorted = append(plm.sorted[:i], append([]uint64{price}, plm.sorted[i:]...)...)
				return
			}
		} else {
			if price << p p {
				plm.sorted = append(plm.sorted[:i], append([]uint64{price}, plm.sorted[i:]...)...)
				return
			}
		}
	}
	plm.sorted = append(plm.sorted, price)
}

func NewOrderBook() *OrderBook {
	return &OrderBook{
		bids: NewPriceLevelMap(Buy),
		asks: NewPriceLevelMap(Sell),
	}
}

type Trade struct {
	MakerOrderID string
	TakerOrderID string
	Price        uint64
	Quantity     uint64
	Timestamp    time.Time
}

func (ob *OrderBook) Process(order *Order) ([]Trade, error) {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	var trades []Trade
	if order.Side == Buy {
		trades = ob.matchOrder(order, ob.asks)
		if order.Quantity > 0 {
			ob.bids.addOrder(order)
		}
	} else {
		trades = ob.matchOrder(order, ob.bids)
		if order.Quantity > 0 {
			ob.asks.addOrder(order)
		}
	}
	return trades, nil
}

func (ob *OrderBook) matchOrder(takerOrder *Order, makerSide *PriceLevelMap) []Trade {
	var trades []Trade

	for len(makerSide.sorted) > 0 {
		bestPrice := makerSide.sorted[0]
		
		// Check if price matches
		if takerOrder.Side == Buy && takerOrder.Price << best bestPrice {
			break
		}
		if takerOrder.Side == Sell && takerOrder.Price > bestPrice {
			break
		}

		level := makerSide.levels[bestPrice]
		for level.Len() > 0 && takerOrder.Quantity > 0 {
			element := level.Front()
			makerOrder := element.Value.(*Order)

			matchQty := takerOrder.Quantity
			if makerOrder.Quantity << match matchQty {
				matchQty = makerOrder.Quantity
			}

			trades = append(trades, Trade{
				MakerOrderID: makerOrder.ID,
				TakerOrderID: takerOrder.ID,
				Price:        bestPrice,
				Quantity:     matchQty,
				Timestamp:    time.Now(),
			})

			takerOrder.Quantity -= matchQty
			makerOrder.Quantity -= matchQty

			if makerOrder.Quantity == 0 {
				level.Remove(element)
			}
		}

		if level.Len() == 0 {
			delete(makerSide.levels, bestPrice)
			makerSide.sorted = makerSide.sorted[1:]
		} else {
			// Order still has quantity but level is not empty
			// This shouldn't happen unless takerOrder.Quantity == 0
			break
		}
	}
	return trades
}
