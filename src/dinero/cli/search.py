import json
import datetime

import click
from loguru import logger
from sqlalchemy import select, extract
from tabulate import tabulate

from dinero.application import Application
from dinero.db import Transaction, get_session


@click.command()
@click.option("--account", default=None, help="Filter by account name (exact match).")
@click.option("--category", default=None, help="Filter by category (exact match).")
@click.option(
    "--subcategory", default=None, help="Filter by subcategory (exact match)."
)
@click.option(
    "--description",
    default=None,
    help="Search description (case-insensitive partial match).",
)
@click.option(
    "--after", default=None, help="Transactions on or after this date (YYYY-MM-DD)."
)
@click.option(
    "--before", default=None, help="Transactions on or before this date (YYYY-MM-DD)."
)
@click.option("--year", type=int, default=None, help="Filter by year.")
@click.option("--month", type=int, default=None, help="Filter by month (1-12).")
@click.option(
    "--limit",
    type=int,
    default=50,
    show_default=True,
    help="Maximum number of rows to return.",
)
@click.option(
    "--sort",
    default="date",
    show_default=True,
    help="Column to sort by: date, amount, description, account, category.",
)
@click.option("--desc/--asc", default=True, show_default=True, help="Sort direction.")
@click.option(
    "--json",
    "json_output",
    is_flag=True,
    default=False,
    help="Output as JSON instead of table.",
)
def search(
    account,
    category,
    subcategory,
    description,
    after,
    before,
    year,
    month,
    limit,
    sort,
    desc,
    json_output,
):
    """Search transactions in the database with filters."""
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
