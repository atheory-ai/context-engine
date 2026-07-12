class NotFoundError(Exception):
    pass


def load_user(user_id):
    # ValueError("msg") -> message; a bare custom class / call with no message ->
    # the exception type name; a bare `raise` re-raise is skipped.
    if user_id == "":
        raise ValueError("empty_id")
    if user_id == "missing":
        raise NotFoundError()
    if user_id == "closed":
        raise NotFoundError
    return user_id


def retry(fn):
    try:
        fn()
    except Exception:
        raise  # re-raise — no stable name, not a declared failure mode
