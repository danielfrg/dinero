"""Compare legacy Python and Go CLI semantics on independent synthetic fixtures.

Run from anywhere: uv run python testdata/compat/check.py --go-executable bin/dinero-go
Every child receives an explicit temporary config before importing dinero.
"""

import argparse
import csv
import json
import os
import sqlite3
import subprocess
import sys
import tempfile
from datetime import datetime
from pathlib import Path


REPO = Path(__file__).resolve().parents[2]
ROWS = [
    (1, "2026-09-01 08:00:00", "Coffee", "Food", "Cafe", 4.25, "first", "Test Account"),
    (2, "2026-09-02 09:00:00", "Coffee", "Food", "Cafe", 4.25, "second", "Test Account"),
    (3, "2026-09-03 10:00:00", "Coffee", "Food", "Cafe", 4.25, "third", "Test Account"),
    (4, "2026-09-02 12:00:00", "Salary", "Income", "Pay", -100.0, "payday", "Test Account"),
    (5, "2026-09-04 18:00:00", "Groceries", None, None, 12.5, None, "Other Account"),
    (6, "2026-09-05 00:00:00", None, None, None, None, None, None),
]
SCHEMA = """CREATE TABLE transactions (
 id INTEGER PRIMARY KEY, date DATETIME, description VARCHAR(1000),
 category VARCHAR(50), subcategory VARCHAR(50), amount DOUBLE,
 notes VARCHAR(1000), account VARCHAR(50))"""
FIELDS = ["id", "date", "description", "category", "subcategory", "amount", "notes", "account"]


def fixture(place: Path, python: bool, query: bool):
    place.mkdir(parents=True)
    xdg = place / "xdg"
    state = xdg / "dinero"
    state.mkdir(parents=True)
    db = place / "input.sqlite"
    with sqlite3.connect(db) as conn:
        conn.execute(SCHEMA)
        if query:
            conn.executemany("INSERT INTO transactions VALUES (?,?,?,?,?,?,?,?)", ROWS)
    (place / "config.toml").write_text(
        'timezone = "UTC"\n[plaid]\nclient_id = ""\nsecret = ""\n'
        'env = "development"\nproducts = "transactions"\n'
        '[plaid.tokens]\n[plaid.account_id_to_name]\ntest = "Test Account"\n'
        f'[database]\nconnection_string = "sqlite:///{db}"\n', encoding="utf-8"
    )
    (state / "category_rules.json").write_text(
        json.dumps({"Coffee": ["Food", "Cafe"]} if not query else {}), encoding="utf-8"
    )
    if not query:
        (place / "transactions.csv").write_text(
            "date,description,amount,category,subcategory\n"
            "2026-09-02,Coffee,4.25,,\n2026-09-03,Salary,-100.00,Income,Pay\n",
            encoding="utf-8",
        )
    env = os.environ.copy()
    for key in ("DINERO_DATABASE_URL", "DINERO_DATABASE_URL_FILE", "DINERO_API_TOKEN_FILE", "DINERO_YES"):
        env.pop(key, None)
    env.update(DINERO_CONFIG_FILE=str(place / "config.toml"), XDG_CONFIG_HOME=str(xdg),
               DINERO_STATE_DIR=str(state), PYTHONPATH=str(REPO / "src"))
    return env


def run(argv, place, env, name):
    done = subprocess.run(argv, cwd=place, env=env, capture_output=True, text=True, check=False)
    if done.returncode:
        raise AssertionError(f"{name}: exit {done.returncode}; stderr={done.stderr.strip()}")
    return done.stdout


def db_rows(path):
    with sqlite3.connect(path) as conn:
        return conn.execute("SELECT * FROM transactions ORDER BY id").fetchall()


def normal_date(value):
    if value in (None, ""):
        return value
    return datetime.fromisoformat(str(value).replace("Z", "+00:00")).replace(tzinfo=None).isoformat(sep=" ", timespec="seconds")


def normal_rows(rows):
    return [tuple(normal_date(v) if i == 1 else v for i, v in enumerate(row)) for row in rows]


def dataset(place):
    with (place / "data" / "all.csv").open(newline="") as handle:
        csv_rows = list(csv.DictReader(handle))
    with sqlite3.connect(place / "data" / "transactions.db") as conn:
        columns = [r[1] for r in conn.execute("PRAGMA table_info(transactions)")]
        rows = conn.execute("SELECT * FROM transactions ORDER BY id").fetchall()
    assert columns == FIELDS, columns
    for row in csv_rows:
        row["date"] = normal_date(row["date"])
        if row["amount"]:
            row["amount"] = float(row["amount"])
    return csv_rows, normal_rows(rows)


def collect(executable, root, python):
    prefix = [executable, "-m", "dinero.cli.main"] if python else [executable]
    query = root / "query"
    env = fixture(query, python, True)
    result = {}
    commands = {
        "search_coffee": ["search", "--description", "coffee", "--before", "2026-09-02", "--asc", "--json"],
        "search_category": ["search", "--category", "Food", "--sort", "date", "--asc", "--json"],
        "search_missing": ["search", "--description", "missing", "--json"],
        "balance": ["balance", "--account", "Test Account", "--through", "2026-09-03", "--json"],
    }
    for name, args in commands.items():
        result[name] = json.loads(run(prefix + args, query, env, name))
    run(prefix + ["build-cache"], query, env, "build-cache")
    cache = query / "xdg" / "dinero" / "cache"
    result["accounts_cache"] = json.loads((cache / "accounts.json").read_text())
    result["categories_cache"] = json.loads((cache / "categories.json").read_text())
    # Go requires explicit --commit; Python's legacy command uses DINERO_YES.
    rule_args = ["mkrules", "--commit", "--yes"] if not python else ["mkrules"]
    rule_env = dict(env, DINERO_YES="1") if python else env
    run(prefix + rule_args, query, rule_env, "mkrules")
    result["rules"] = json.loads((query / "xdg" / "dinero" / "category_rules.json").read_text())
    run(prefix + ["mkdataset"], query, env, "mkdataset")
    result["dataset"] = dataset(query)

    imports = root / "import"
    import_env = fixture(imports, python, False)
    result["import_before"] = db_rows(imports / "input.sqlite")
    for name, flags in (("preview", []), ("commit", ["--commit", "--yes"]), ("repeat", ["--commit", "--yes"])):
        result["import_" + name] = json.loads(run(prefix + ["import-csv", "transactions.csv", "Test Account", *flags, "--json"], imports, import_env, name))
        result["import_" + name + "_db"] = normal_rows(db_rows(imports / "input.sqlite"))
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--python-executable", default=sys.executable)
    parser.add_argument("--go-executable", required=True, type=Path)
    args = parser.parse_args()
    go = args.go_executable.resolve()
    if not go.is_file():
        parser.error(f"Go executable not found: {go}")
    with tempfile.TemporaryDirectory(prefix="dinero-compat-") as tmp:
        root = Path(tmp)
        old = collect(args.python_executable, root / "python", True)
        new = collect(str(go), root / "go", False)
        failures = [name for name in old if old[name] != new[name]]
        if failures:
            for name in failures:
                print(f"MISMATCH {name}: Python={old[name]!r} Go={new[name]!r}", file=sys.stderr)
            return 1
        print(f"PASS {len(old)} parity comparisons: search, balance, cache, rules, export, CSV import")
    return 0


if __name__ == "__main__":
    sys.exit(main())
