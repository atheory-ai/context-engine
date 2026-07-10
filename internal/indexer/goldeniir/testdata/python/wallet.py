def find_user(id: str, limit: int) -> str:
    if id is None:
        raise ValueError("missing_id")
    return id

class Wallet:
    def charge(self, amount: int) -> bool:
        return True
