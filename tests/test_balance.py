import datetime
import json

from click.testing import CliRunner

from dinero.cli.balance import balance
from dinero.cli.main import main
from dinero.cli.search import search

from conftest import add_transaction


def test_balance_includes_entire_through_date(dinero_database):
    add_transaction(
        dinero_database,
        date=datetime.datetime(2026, 9, 2, 0, 0),
        description="Midnight",
        amount=10.00,
    )
    add_transaction(
        dinero_database,
        date=datetime.datetime(2026, 9, 2, 23, 59, 59, 999999),
        description="End of day",
        amount=5.25,
    )
    add_transaction(
        dinero_database,
        date=datetime.datetime(2026, 9, 3, 0, 0),
        description="Next day",
        amount=7.00,
    )

    result = CliRunner().invoke(
        balance,
        ["--account", "Test Account", "--through", "2026-09-02", "--json"],
    )

    assert result.exit_code == 0
    assert json.loads(result.stdout) == {
        "account": "Test Account",
        "through": "2026-09-02",
        "amount": "15.25",
    }


def test_search_before_is_inclusive_and_json_is_clean(dinero_database):
    add_transaction(
        dinero_database,
        date=datetime.datetime(2026, 9, 2, 12, 0),
        description="Closing day",
        amount=3.50,
    )
    add_transaction(
        dinero_database,
        date=datetime.datetime(2026, 9, 3, 0, 0),
        description="Next day",
        amount=4.50,
    )

    result = CliRunner().invoke(search, ["--before", "2026-09-02", "--json"])

    assert result.exit_code == 0
    rows = json.loads(result.stdout)
    assert [row["description"] for row in rows] == ["Closing day"]


def test_empty_search_writes_json_array(dinero_database):
    result = CliRunner().invoke(search, ["--json"])

    assert result.exit_code == 0
    assert json.loads(result.stdout) == []


def test_top_level_json_stdout_is_parseable(dinero_database):
    result = CliRunner().invoke(main, ["search", "--json"])

    assert result.exit_code == 0
    assert json.loads(result.stdout) == []
