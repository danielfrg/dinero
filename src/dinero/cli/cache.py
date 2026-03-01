import json
import os

import click
from loguru import logger
from sqlalchemy import select

from dinero.application import Application
from dinero.db import Transaction, get_session


@click.command()
def cache():
    """Cache accounts, categories and subcategories.

    Creates JSON files in ~/.config/dinero/cache/ that can be used by
    AI agents or other tools to know valid filter values without querying
    the database directly.
    """
    app = Application()
    session = get_session(app)

    cache_dir = app.cache_dir
    os.makedirs(cache_dir, exist_ok=True)

    # Query distinct accounts
    stmt = (
        select(Transaction.account)
        .where(Transaction.account.isnot(None))
        .where(Transaction.account != "")
        .distinct()
        .order_by(Transaction.account)
    )
    accounts = [row for row in session.scalars(stmt)]

    # Query distinct categories
    stmt = (
        select(Transaction.category)
        .where(Transaction.category.isnot(None))
        .where(Transaction.category != "")
        .distinct()
        .order_by(Transaction.category)
    )
    categories = [row for row in session.scalars(stmt)]

    # Query distinct subcategories
    stmt = (
        select(Transaction.subcategory)
        .where(Transaction.subcategory.isnot(None))
        .where(Transaction.subcategory != "")
        .distinct()
        .order_by(Transaction.subcategory)
    )
    subcategories = [row for row in session.scalars(stmt)]

    # Query distinct (category, subcategory) pairs for richer context
    stmt = (
        select(Transaction.category, Transaction.subcategory)
        .where(Transaction.category.isnot(None))
        .where(Transaction.category != "")
        .where(Transaction.subcategory.isnot(None))
        .where(Transaction.subcategory != "")
        .distinct()
        .order_by(Transaction.category, Transaction.subcategory)
    )
    category_pairs = [
        {"category": row[0], "subcategory": row[1]} for row in session.execute(stmt)
    ]

    # Write accounts.json
    accounts_path = cache_dir / "accounts.json"
    with open(accounts_path, "w") as f:
        json.dump(accounts, f, indent=2)
    logger.info(f"Cached {len(accounts)} accounts to {accounts_path}")

    # Write categories.json
    categories_data = {
        "categories": categories,
        "subcategories": subcategories,
        "category_subcategory_pairs": category_pairs,
    }
    categories_path = cache_dir / "categories.json"
    with open(categories_path, "w") as f:
        json.dump(categories_data, f, indent=2)
    logger.info(
        f"Cached {len(categories)} categories and {len(subcategories)} subcategories to {categories_path}"
    )

    session.close()

    logger.info(f"Cache written to {cache_dir}")
