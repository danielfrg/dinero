import datetime
import re

import pendulum
from sqlalchemy import select

from dinero.db import Transaction


ISO_DATE_PATTERN = re.compile(r"\d{4}-\d{2}-\d{2}")


def start_of_day(value: str, timezone: str):
    """Return local midnight for a strict YYYY-MM-DD value."""
    if not ISO_DATE_PATTERN.fullmatch(value):
        raise ValueError(f"Invalid date: '{value}'. Expected YYYY-MM-DD")

    try:
        day = datetime.date.fromisoformat(value)
    except ValueError as exc:
        raise ValueError(f"Invalid date: '{value}'. Expected YYYY-MM-DD") from exc

    return pendulum.datetime(day.year, day.month, day.day, tz=timezone)


def start_of_next_day(value: str, timezone: str):
    """Return local midnight after a strict YYYY-MM-DD value."""
    return start_of_day(value, timezone).add(days=1)


def known_account_names(app, session) -> set[str]:
    """Return account names known by Plaid configuration or the database."""
    configured = set(app.config.plaid.account_id_to_name.values())
    stmt = (
        select(Transaction.account)
        .where(Transaction.account.isnot(None))
        .where(Transaction.account != "")
        .distinct()
    )
    return configured | set(session.scalars(stmt))
