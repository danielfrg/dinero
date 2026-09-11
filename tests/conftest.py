import datetime

import pytest
from sqlalchemy import create_engine
from sqlalchemy.orm import Session

from dinero.db import Base, Transaction


@pytest.fixture
def dinero_database(tmp_path, monkeypatch):
    database_path = tmp_path / "dinero.sqlite"
    config_path = tmp_path / "config.toml"
    config_path.write_text(
        f"""timezone = "UTC"

[plaid]
client_id = ""
secret = ""
env = "development"
products = "transactions"

[plaid.tokens]

[plaid.account_id_to_name]
test = "Test Account"

[database]
connection_string = "sqlite:///{database_path}"
""",
        encoding="utf-8",
    )
    monkeypatch.setenv("DINERO_CONFIG_FILE", str(config_path))

    engine = create_engine(f"sqlite:///{database_path}")
    Base.metadata.create_all(engine)
    return engine


def add_transaction(
    engine,
    *,
    date: datetime.datetime,
    description: str,
    amount: float,
    account: str = "Test Account",
):
    with Session(engine) as session:
        session.add(
            Transaction(
                date=date,
                description=description,
                amount=amount,
                account=account,
                category="",
                subcategory="",
                notes="test",
            )
        )
        session.commit()
