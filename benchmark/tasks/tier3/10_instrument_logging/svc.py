import logging


def fetch(user_id):
    logging.info(f"enter fetch args={user_id}")
    result = {"id": user_id, "name": "alice"}
    logging.info(f"exit fetch result={result}")
    return result


def transform(record):
    logging.info(f"enter transform args={record}")
    result = {**record, "name": record["name"].upper()}
    logging.info(f"exit transform result={result}")
    return result


def save(record):
    logging.info(f"enter save args={record}")
    # pretend this writes to a DB
    result = True
    logging.info(f"exit save result={result}")
    return result


def pipeline(user_id):
    r = fetch(user_id)
    r = transform(r)
    return save(r)


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO, format="%(levelname)s %(message)s")
    pipeline(42)
