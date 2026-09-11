"""Import transactions from a CSV file into the database.

CSV format expected:
    date,description,amount[,category,subcategory]

Example:
    date,description,amount
    2026-01-31,Deposits,5000.00
    2026-01-31,Withdrawals,-1200.00

Usage:
    dinero import-csv ./transactions.csv "Brokerage Account"
    dinero import-csv ./transactions.csv "Brokerage Account" --commit
"""

import csv
import datetime
import json
import math
import re
from collections import defaultdict
from decimal import Decimal, InvalidOperation
from pathlib import Path

import click
import pendulum
from sqlalchemy.exc import SQLAlchemyError

from dinero import Application, db, rules
from dinero.query import known_account_names


CENT = Decimal("0.01")
MONEY_PATTERN = re.compile(r"[+-]?(?:\d+|\d{1,3}(?:,\d{3})+)(?:\.\d+)?")


@click.command()
@click.argument("file")
@click.argument("account")
@click.option(
    "--commit/--dry-run",
    default=False,
    show_default=True,
    help="Write transactions or only preview the import.",
)
@click.option(
    "--yes",
    is_flag=True,
    help="Skip confirmation when used with --commit.",
)
@click.option(
    "--json",
    "json_output",
    is_flag=True,
    default=False,
    help="Output the import result as JSON.",
)
def import_csv(file: str, account: str, commit: bool, yes: bool, json_output: bool):
    """Import transactions from a CSV file.

    FILE is the path to the CSV file to import.

    ACCOUNT is the account name to associate with all transactions.
    """
    csv_path = Path(file)

    if not csv_path.exists():
        raise click.ClickException(f"File not found: {csv_path}")

    if yes and not commit:
        raise click.UsageError("--yes requires --commit")

    app = Application()
    table = db.Table(app)

    try:
        if account not in known_account_names(app, table.session):
            raise click.ClickException(f"Unknown account: {account}")

        try:
            transactions = parse_csv(app, csv_path, account)
        except ValueError as exc:
            raise click.ClickException(str(exc)) from exc

        if not transactions:
            result = import_result(csv_path, account, [], [], [], "no-transactions")
            write_result(result, json_output)
            return

        duplicates = duplicate_input_rows(transactions, app.config.timezone)
        if duplicates:
            details = "; ".join(
                f"rows {', '.join(map(str, rows))}" for rows in duplicates
            )
            raise click.ClickException(f"Duplicate CSV transactions: {details}")

        new, existing, ambiguous = classify_transactions(
            table, transactions, app.config.timezone
        )
        result = import_result(csv_path, account, new, existing, ambiguous, "dry-run")

        if ambiguous:
            result["status"] = "ambiguous"
            write_result(result, json_output)
            raise click.exceptions.Exit(1)

        if not new:
            result["status"] = "no-new-transactions"
            write_result(result, json_output)
            return

        if not commit:
            write_result(result, json_output)
            return

        if json_output and not yes:
            raise click.UsageError("--json --commit requires --yes")

        preview_written = False
        if not yes:
            write_result(result, False)
            preview_written = True
            click.confirm("Import these transactions?", default=False, abort=True)

        table.new = new
        table.existing = existing
        try:
            table.commit()
        except SQLAlchemyError as exc:
            table.session.rollback()
            raise click.ClickException(
                f"Import failed; no transactions were committed: {exc}"
            ) from exc
        result["status"] = "imported"
        if preview_written:
            click.echo(f"Imported {len(new)} transaction(s).")
        else:
            write_result(result, json_output)
    finally:
        table.close()


def write_result(result: dict, json_output: bool):
    if json_output:
        click.echo(json.dumps(result, indent=2))
        return

    print("=" * 80)
    print(f"Import from: {result['file']}")
    print(f"Account: {result['account']}")
    print("=" * 80)
    print(f"Status: {result['status']}")
    print(f"Transactions in CSV: {result['transactions_in_csv']}")
    print(f"New transactions: {result['new_transactions']}")
    print(f"Already existing (will skip): {result['existing_transactions']}")
    print(f"Ambiguous transactions: {result['ambiguous_transactions']}")
    print()

    if result["transactions"]:
        print("-" * 80)
        print("New transactions:")
        print("-" * 80)
        for transaction in result["transactions"]:
            category = transaction["category"]
            category_str = f" [{category}]" if category else ""
            print(
                f"  {transaction['date']}  {transaction['amount']:>10}  "
                f"{transaction['description']}{category_str}"
            )
        print()

    for item in result["ambiguous"]:
        transaction = item["transaction"]
        print(
            f"Ambiguous: CSV row {transaction['row']} matches database IDs "
            f"{', '.join(map(str, item['existing_ids']))}"
        )


def import_result(csv_path, account, new, existing, ambiguous, status):
    return {
        "status": status,
        "file": str(csv_path),
        "account": account,
        "transactions_in_csv": len(new) + len(existing) + len(ambiguous),
        "new_transactions": len(new),
        "existing_transactions": len(existing),
        "ambiguous_transactions": len(ambiguous),
        "transactions": [serialize_transaction(transaction) for transaction in new],
        "ambiguous": ambiguous,
    }


def serialize_transaction(transaction):
    return {
        "row": getattr(transaction, "_csv_row_num", None),
        "date": transaction.date.strftime("%Y-%m-%d"),
        "description": transaction.description,
        "amount": format(decimal_amount(transaction.amount), ".2f"),
        "category": transaction.category or "",
        "subcategory": transaction.subcategory or "",
    }


def transaction_key(transaction, timezone):
    date = transaction.date
    if isinstance(date, datetime.datetime):
        if date.tzinfo is not None:
            date = pendulum.instance(date).in_timezone(timezone)
        date = date.date()
    return (
        date,
        transaction.account,
        transaction.description,
        decimal_amount(transaction.amount),
    )


def decimal_amount(value) -> Decimal:
    return Decimal(str(value)).quantize(CENT)


def duplicate_input_rows(transactions, timezone):
    rows_by_key = defaultdict(list)
    for transaction in transactions:
        rows_by_key[transaction_key(transaction, timezone)].append(
            getattr(transaction, "_csv_row_num")
        )
    return [rows for rows in rows_by_key.values() if len(rows) > 1]


def classify_transactions(table, transactions, timezone):
    existing_by_key = defaultdict(list)
    for transaction in table.records:
        if (
            transaction.date is not None
            and transaction.account is not None
            and transaction.description is not None
            and transaction.amount is not None
        ):
            existing_by_key[transaction_key(transaction, timezone)].append(transaction)

    new = []
    existing = []
    ambiguous = []
    for transaction in transactions:
        matches = existing_by_key[transaction_key(transaction, timezone)]
        if len(matches) > 1:
            ambiguous.append(
                {
                    "transaction": serialize_transaction(transaction),
                    "existing_ids": [match.id for match in matches],
                }
            )
        elif matches:
            existing.append(transaction)
        else:
            new.append(transaction)
    return new, existing, ambiguous


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
            raise ValueError(
                f"Missing required columns: {', '.join(sorted(missing))}. "
                "Required columns: date, description, amount"
            )

        for row_num, row in enumerate(
            reader, start=2
        ):  # start=2 because row 1 is header
            try:
                transaction = parse_row(
                    app,
                    row,
                    account,
                    row_num,
                    source=f"csv-import:{csv_path.name}",
                )
                if transaction:
                    transactions.append(transaction)
            except ValueError as e:
                raise ValueError(f"Row {row_num}: {e}") from e

    return transactions


def parse_row(
    app: Application, row: dict, account: str, row_num: int, source: str = "csv-import"
) -> db.Transaction | None:
    """Parse a single CSV row into a Transaction object."""
    date_str = (row.get("date") or "").strip()
    description = (row.get("description") or "").strip()
    amount_str = (row.get("amount") or "").strip()

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

    # Parse a strict ISO date rather than accepting pendulum's broader formats.
    if len(date_str) != 10 or date_str[4] != "-" or date_str[7] != "-":
        raise ValueError(f"Invalid date format: '{date_str}'. Expected YYYY-MM-DD")
    try:
        parsed_date = datetime.date.fromisoformat(date_str)
        date = pendulum.datetime(
            parsed_date.year,
            parsed_date.month,
            parsed_date.day,
            tz=app.config.timezone,
        )
    except ValueError:
        raise ValueError(
            f"Invalid date format: '{date_str}'. Expected ISO format (YYYY-MM-DD)"
        ) from None

    # Parse and validate money as Decimal. The existing database column remains Double.
    if not MONEY_PATTERN.fullmatch(amount_str):
        raise ValueError(f"Invalid amount: '{amount_str}'")
    try:
        amount = Decimal(amount_str.replace(",", ""))
    except InvalidOperation:
        raise ValueError(f"Invalid amount: '{amount_str}'") from None
    if not amount.is_finite():
        raise ValueError(f"Invalid amount: '{amount_str}'")
    try:
        rounded_amount = amount.quantize(CENT)
    except InvalidOperation:
        raise ValueError(f"Invalid amount: '{amount_str}'") from None
    if amount != rounded_amount:
        raise ValueError(
            f"Invalid amount: '{amount_str}'. Expected at most two decimals"
        )
    stored_amount = float(amount)
    if not math.isfinite(stored_amount) or decimal_amount(stored_amount) != amount:
        raise ValueError(f"Invalid amount: '{amount_str}'. Cannot preserve cents")

    # Get category from CSV or from rules
    category = (row.get("category") or "").strip()
    subcategory = (row.get("subcategory") or "").strip()

    if not category:
        category, subcategory = rules.categories_for_transaction(app, description)

    # Create Transaction object
    t = db.Transaction()
    t.date = date
    t.description = description
    t.amount = stored_amount
    t.category = category
    t.subcategory = subcategory
    t.notes = source
    t.account = account
    setattr(t, "_csv_row_num", row_num)

    return t
