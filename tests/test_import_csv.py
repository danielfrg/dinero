import datetime
import json

from click.testing import CliRunner
from sqlalchemy import func, select
from sqlalchemy.exc import SQLAlchemyError
from sqlalchemy.orm import Session

from dinero.cli.import_csv import import_csv, transaction_key
from dinero import db
from dinero.db import Transaction

from conftest import add_transaction


def write_csv(tmp_path, rows):
    csv_path = tmp_path / "transactions.csv"
    csv_path.write_text(
        "date,description,amount\n" + "\n".join(rows) + "\n",
        encoding="utf-8",
    )
    return csv_path


def transaction_count(engine):
    with Session(engine) as session:
        return session.scalar(select(func.count(Transaction.id)))


def test_import_defaults_to_dry_run(dinero_database, tmp_path):
    csv_path = write_csv(tmp_path, ["2026-09-02,Coffee,4.25"])

    result = CliRunner().invoke(import_csv, [str(csv_path), "Test Account", "--json"])

    assert result.exit_code == 0
    assert json.loads(result.stdout)["status"] == "dry-run"
    assert transaction_count(dinero_database) == 0


def test_commit_records_csv_provenance(dinero_database, tmp_path):
    csv_path = write_csv(tmp_path, ["2026-09-02,Coffee,4.25"])

    result = CliRunner().invoke(
        import_csv,
        [str(csv_path), "Test Account", "--commit", "--yes", "--json"],
    )

    assert result.exit_code == 0
    assert json.loads(result.stdout)["status"] == "imported"
    with Session(dinero_database) as session:
        transaction = session.scalar(select(Transaction))
        assert transaction is not None
        assert transaction.notes == "csv-import:transactions.csv"
        assert transaction.amount == 4.25


def test_import_rejects_unknown_account(dinero_database, tmp_path):
    csv_path = write_csv(tmp_path, ["2026-09-02,Coffee,4.25"])

    result = CliRunner().invoke(import_csv, [str(csv_path), "Unknown"])

    assert result.exit_code == 1
    assert "Unknown account: Unknown" in result.output
    assert transaction_count(dinero_database) == 0


def test_import_rejects_duplicate_csv_rows(dinero_database, tmp_path):
    csv_path = write_csv(
        tmp_path,
        ["2026-09-02,Coffee,4.25", "2026-09-02,Coffee,4.25"],
    )

    result = CliRunner().invoke(import_csv, [str(csv_path), "Test Account"])

    assert result.exit_code == 1
    assert "Duplicate CSV transactions: rows 2, 3" in result.output
    assert transaction_count(dinero_database) == 0


def test_import_reports_ambiguous_database_matches(dinero_database, tmp_path):
    for _ in range(2):
        add_transaction(
            dinero_database,
            date=datetime.datetime(2026, 9, 2),
            description="Coffee",
            amount=4.25,
        )
    csv_path = write_csv(tmp_path, ["2026-09-02,Coffee,4.25"])

    result = CliRunner().invoke(import_csv, [str(csv_path), "Test Account", "--json"])

    assert result.exit_code == 1
    output = json.loads(result.stdout)
    assert output["status"] == "ambiguous"
    assert output["ambiguous_transactions"] == 1
    assert len(output["ambiguous"][0]["existing_ids"]) == 2


def test_import_rejects_non_money_decimal(dinero_database, tmp_path):
    csv_path = write_csv(tmp_path, ["2026-09-02,Coffee,4.251"])

    result = CliRunner().invoke(import_csv, [str(csv_path), "Test Account"])

    assert result.exit_code == 1
    assert "Expected at most two decimals" in result.output
    assert transaction_count(dinero_database) == 0


def test_import_rejects_malformed_thousands_separator(dinero_database, tmp_path):
    csv_path = write_csv(tmp_path, ['2026-09-02,Coffee,"1,2"'])

    result = CliRunner().invoke(import_csv, [str(csv_path), "Test Account"])

    assert result.exit_code == 1
    assert "Invalid amount: '1,2'" in result.output
    assert transaction_count(dinero_database) == 0


def test_import_rejects_amount_that_cannot_preserve_cents(dinero_database, tmp_path):
    csv_path = write_csv(tmp_path, ["2026-09-02,Coffee,999999999999999.99"])

    result = CliRunner().invoke(import_csv, [str(csv_path), "Test Account"])

    assert result.exit_code == 1
    assert "Cannot preserve cents" in result.output
    assert transaction_count(dinero_database) == 0


def test_import_commit_defaults_confirmation_to_no(dinero_database, tmp_path):
    csv_path = write_csv(tmp_path, ["2026-09-02,Coffee,4.25"])

    result = CliRunner().invoke(
        import_csv,
        [str(csv_path), "Test Account", "--commit"],
        input="\n",
    )

    assert result.exit_code == 1
    assert "Import these transactions? [y/N]" in result.output
    assert transaction_count(dinero_database) == 0


def test_yes_requires_commit(dinero_database, tmp_path):
    csv_path = write_csv(tmp_path, ["2026-09-02,Coffee,4.25"])

    result = CliRunner().invoke(import_csv, [str(csv_path), "Test Account", "--yes"])

    assert result.exit_code == 2
    assert "--yes requires --commit" in result.output
    assert transaction_count(dinero_database) == 0


def test_transaction_key_normalizes_aware_dates_to_configured_timezone():
    transaction = Transaction(
        date=datetime.datetime(2026, 9, 3, 4, 30, tzinfo=datetime.timezone.utc),
        description="Coffee",
        amount=4.25,
        account="Test Account",
    )

    key = transaction_key(transaction, "America/Chicago")

    assert key[0] == datetime.date(2026, 9, 2)


def test_import_rolls_back_and_reports_database_errors(
    dinero_database, tmp_path, monkeypatch
):
    csv_path = write_csv(tmp_path, ["2026-09-02,Coffee,4.25"])

    def fail_commit(table):
        table.session.add_all(table.new)
        raise SQLAlchemyError("database unavailable")

    monkeypatch.setattr(db.Table, "commit", fail_commit)

    result = CliRunner().invoke(
        import_csv,
        [str(csv_path), "Test Account", "--commit", "--yes"],
    )

    assert result.exit_code == 1
    assert "Import failed; no transactions were committed" in result.output
    assert transaction_count(dinero_database) == 0
