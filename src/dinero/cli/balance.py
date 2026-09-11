import json
from decimal import Decimal

import click
from sqlalchemy import func, select

from dinero import Application
from dinero.db import Transaction, get_session
from dinero.query import known_account_names, start_of_next_day


CENT = Decimal("0.01")


@click.command()
@click.option("--account", required=True, help="Account name (exact match).")
@click.option(
    "--through",
    required=True,
    help="Include transactions through the end of this date (YYYY-MM-DD).",
)
@click.option(
    "--json",
    "json_output",
    is_flag=True,
    default=False,
    help="Output as JSON.",
)
def balance(account: str, through: str, json_output: bool):
    """Compute an account's cumulative transaction amount through a date."""
    app = Application()
    session = get_session(app)

    try:
        if account not in known_account_names(app, session):
            raise click.ClickException(f"Unknown account: {account}")

        try:
            end_exclusive = start_of_next_day(through, app.config.timezone)
        except ValueError as exc:
            raise click.BadParameter(str(exc), param_hint="--through") from exc

        amount = session.scalar(
            select(func.sum(Transaction.amount)).where(
                Transaction.account == account,
                Transaction.date < end_exclusive,
            )
        )
        rounded = Decimal(str(amount or 0)).quantize(CENT)

        if json_output:
            click.echo(
                json.dumps(
                    {
                        "account": account,
                        "through": through,
                        "amount": format(rounded, ".2f"),
                    }
                )
            )
        else:
            click.echo(f"{account}\t{through}\t{rounded:.2f}")
    finally:
        session.close()
