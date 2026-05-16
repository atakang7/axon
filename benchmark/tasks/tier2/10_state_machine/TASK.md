# Task: state machine

Implement a vending-machine state machine in `vending.py` exposing class `Vending`:
- States: `IDLE`, `HAS_COIN`, `DISPENSING`.
- Methods (each returns the new state name as a string):
  - `insert_coin()`: IDLE → HAS_COIN. From HAS_COIN: stays HAS_COIN. From DISPENSING: rejected (raise ValueError).
  - `select(item)`: HAS_COIN → DISPENSING. From IDLE/DISPENSING: raise ValueError.
  - `dispense()`: DISPENSING → IDLE. Otherwise raise ValueError.
  - `state` property: current state.

## Success
- All transitions above work.
- `python3 -c "from vending import Vending; v=Vending(); v.insert_coin(); v.select('soda'); v.dispense(); print(v.state)"` prints `IDLE`.
- Calling `dispense()` first raises ValueError.
