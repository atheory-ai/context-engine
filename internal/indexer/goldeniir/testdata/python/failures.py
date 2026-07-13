class NotFoundError(Exception):
    pass


def load_user(user_id):
    # ValueError("msg") -> constructed (message); a bare custom class / call with
    # no message -> sentinel (exception type); a bare `raise` re-raise ->
    # propagated.
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
        raise  # re-raise — a propagated failure (anonymous, code "propagated")
