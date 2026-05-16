class Vending:
    IDLE = "IDLE"
    HAS_COIN = "HAS_COIN"
    DISPENSING = "DISPENSING"

    def __init__(self):
        self._state = self.IDLE

    @property
    def state(self):
        return self._state

    def insert_coin(self):
        if self._state == self.DISPENSING:
            raise ValueError("Cannot insert coin while dispensing")

        if self._state == self.IDLE:
            self._state = self.HAS_COIN
        # If already HAS_COIN, stay in HAS_COIN (as per spec)

        return self._state

    def select(self, item):
        if self._state != self.HAS_COIN:
            raise ValueError("Cannot select item without coin")

        self._state = self.DISPENSING
        return self._state

    def dispense(self):
        if self._state != self.DISPENSING:
            raise ValueError("Cannot dispense without selecting item first")

        self._state = self.IDLE
        return self._state
