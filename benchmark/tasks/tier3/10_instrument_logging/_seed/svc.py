def fetch(user_id):
    return {"id": user_id, "name": "alice"}

def transform(record):
    return {**record, "name": record["name"].upper()}

def save(record):
    # pretend this writes to a DB
    return True

def pipeline(user_id):
    r = fetch(user_id)
    r = transform(r)
    return save(r)
