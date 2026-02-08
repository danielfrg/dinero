"""Import transactions from a CSV file into the database.

CSV format expected:
    date,description,amount[,category,subcategory]

Example:
    date,description,amount
    2026-01-31,Deposits,5000.00
    2026-01-31,Withdrawals,-1200.00

Usage:
    dinero import-csv ./transactions.csv --account "Brokerage Account"
"""

import csv
import sys
from pathlib import Path

import pendulum
from loguru import logger

from dinero import Application, db, rules
from dinero.cli import utils


def import_csv(file: str, account: str):
    """Import transactions from a CSV file.

    Parameters
    ----------
    file : str
        Path to the CSV file to import.
    account : str
        Account name to associate with all transactions.
    """
    app = Application()
    csv_path = Path(file)

    if not csv_path.exists():
        logger.error(f"File not found: {csv_path}")
        sys.exit(1)

    # Parse CSV file
    transactions = parse_csv(app, csv_path, account)

    if not transactions:
        logger.warning("No transactions found in CSV file")
        return

    # Load existing transactions and check for duplicates
    table = db.Table(app)
    new, existing = table.prepare_from_records(transactions)

    # Display summary
    print("=" * 80)
    print(f"Import from: {csv_path}")
    print(f"Account: {account}")
    print("=" * 80)
    print(f"Transactions in CSV: {len(transactions)}")
    print(f"New transactions: {len(new)}")
    print(f"Already existing (will skip): {len(existing)}")
    print()

    if not new:
        print("No new transactions to import.")
        return

    # Show transactions to be imported
    print("-" * 80)
    print("Transactions to be imported:")
    print("-" * 80)
    for t in new:
        category_str = f" [{t.category}]" if t.category else ""
        print(
            f"  {t.date.strftime('%Y-%m-%d')}  {t.amount:>10.2f}  {t.description}{category_str}"
        )
    print()

    # Confirm and commit
    if utils.noninteractive() or utils.query_yes_no("Import these transactions?"):
        print(f"Inserting {len(new)} new records...")
        table.commit()
        print("Done")
    else:
        print("Import cancelled.")


def parse_csv(app: Application, csv_path: Path, account: str) -> list[db.Transaction]:
    """Parse a CSV file and return a list of Transaction objects.

    Expected columns: date, description, amount
    Optional columns: category, subcategory
    """
    transactions = []

    with open(csv_path, newline="", encoding="utf-8") as f:
        reader = csv.DictReader(f)

        # Validate required columns
        required_columns = {"date", "description", "amount"}
        if not required_columns.issubset(set(reader.fieldnames or [])):
            missing = required_columns - set(reader.fieldnames or [])
            logger.error(f"Missing required columns: {missing}")
            logger.info("Required columns: date, description, amount")
            sys.exit(1)

        for row_num, row in enumerate(
            reader, start=2
        ):  # start=2 because row 1 is header
            try:
                transaction = parse_row(app, row, account, row_num)
                if transaction:
                    transactions.append(transaction)
            except ValueError as e:
                logger.error(f"Row {row_num}: {e}")
                sys.exit(1)

    return transactions


def parse_row(
    app: Application, row: dict, account: str, row_num: int
) -> db.Transaction | None:
    """Parse a single CSV row into a Transaction object."""
    date_str = row.get("date", "").strip()
    description = row.get("description", "").strip()
    amount_str = row.get("amount", "").strip()

    # Skip empty rows
    if not date_str and not description and not amount_str:
        return None

    # Validate required fields
    if not date_str:
        raise ValueError("Missing date")
    if not description:
        raise ValueError("Missing description")
    if not amount_str:
        raise ValueError("Missing amount")

    # Parse date (ISO format: YYYY-MM-DD)
    try:
        date = pendulum.parse(date_str, tz=app.config.timezone)
    except Exception:
        raise ValueError(
            f"Invalid date format: '{date_str}'. Expected ISO format (YYYY-MM-DD)"
        )

    # Parse amount
    try:
        amount = float(amount_str.replace(",", ""))
    except ValueError:
        raise ValueError(f"Invalid amount: '{amount_str}'")

    # Get category from CSV or from rules
    category = row.get("category", "").strip()
    subcategory = row.get("subcategory", "").strip()

    if not category:
        category, subcategory = rules.categories_for_transaction(app, description)

    # Create Transaction object
    t = db.Transaction()
    t.date = date
    t.description = description
    t.amount = amount
    t.category = category
    t.subcategory = subcategory
    t.notes = "csv-import"
    t.account = account

    return t


if __name__ == "__main__":
    import fire

    fire.Fire(import_csv)
