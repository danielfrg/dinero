import json
import datetime

from loguru import logger
from sqlalchemy import select, extract
from tabulate import tabulate

from dinero.application import Application
from dinero.db import Transaction, get_session


def search(
    account=None,
    category=None,
    subcategory=None,
    description=None,
    after=None,
    before=None,
    year=None,
    month=None,
    limit=50,
    sort="date",
    desc=True,
    json_output=False,
):
    """Search transactions in the database with filters.

    Parameters
    ----------
    account : str, optional
        Filter by account name (exact match)
    category : str, optional
        Filter by category (exact match)
    subcategory : str, optional
        Filter by subcategory (exact match)
    description : str, optional
        Search description (case-insensitive partial match)
    after : str, optional
        Transactions on or after this date (YYYY-MM-DD)
    before : str, optional
        Transactions on or before this date (YYYY-MM-DD)
    year : int, optional
        Filter by year
    month : int, optional
        Filter by month (1-12)
    limit : int, optional
        Maximum number of rows to return (default: 50)
    sort : str, optional
        Column to sort by (default: date). Options: date, amount, description, account, category
    desc : bool, optional
        Sort descending (default: True, most recent first)
    json_output : bool, optional
        Output as JSON instead of table (default: False)
    """
    app = Application()
    session = get_session(app)

    stmt = select(Transaction)

    # Apply filters
    if account is not None:
        stmt = stmt.where(Transaction.account == account)

    if category is not None:
        stmt = stmt.where(Transaction.category == category)

    if subcategory is not None:
        stmt = stmt.where(Transaction.subcategory == subcategory)

    if description is not None:
        stmt = stmt.where(Transaction.description.ilike(f"%{description}%"))

    if after is not None:
        after_date = datetime.datetime.strptime(after, "%Y-%m-%d")
        stmt = stmt.where(Transaction.date >= after_date)

    if before is not None:
        before_date = datetime.datetime.strptime(before, "%Y-%m-%d")
        stmt = stmt.where(Transaction.date <= before_date)

    if year is not None:
        stmt = stmt.where(extract("year", Transaction.date) == year)

    if month is not None:
        stmt = stmt.where(extract("month", Transaction.date) == month)

    # Sorting
    sort_column_map = {
        "date": Transaction.date,
        "amount": Transaction.amount,
        "description": Transaction.description,
        "account": Transaction.account,
        "category": Transaction.category,
        "subcategory": Transaction.subcategory,
    }

    sort_col = sort_column_map.get(sort, Transaction.date)
    if desc:
        stmt = stmt.order_by(sort_col.desc())
    else:
        stmt = stmt.order_by(sort_col.asc())

    # Limit
    stmt = stmt.limit(limit)

    # Execute
    results = session.scalars(stmt).all()

    if not results:
        logger.info("No transactions found matching the given filters.")
        session.close()
        return

    # Format output
    rows = []
    for t in results:
        date_str = t.date.strftime("%Y-%m-%d") if t.date else ""
        rows.append(
            {
                "id": t.id,
                "date": date_str,
                "description": t.description or "",
                "category": t.category or "",
                "subcategory": t.subcategory or "",
                "amount": t.amount,
                "account": t.account or "",
                "notes": t.notes or "",
            }
        )

    if json_output:
        print(json.dumps(rows, indent=2))
    else:
        headers = [
            "id",
            "date",
            "description",
            "category",
            "subcategory",
            "amount",
            "account",
            "notes",
        ]
        table_data = [[row[h] for h in headers] for row in rows]
        print(tabulate(table_data, headers=headers, tablefmt="simple", floatfmt=",.2f"))

    logger.info(f"Found {len(results)} transaction(s)")
    session.close()
