def grade(score):
    # match/case -> one behavior clause per case; `case _` -> else.
    match score:
        case 100:
            return "perfect"
        case 0:
            return "zero"
        case _:
            return "other"


def describe(n):
    # if/elif/else -> three behavior clauses (elif was previously missed).
    if n < 0:
        return "negative"
    elif n == 0:
        return "zero"
    else:
        return "positive"
