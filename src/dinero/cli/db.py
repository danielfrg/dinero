import click
from loguru import logger
from sqlalchemy import create_engine
from sqlalchemy_utils import create_database, database_exists

from dinero import Application
from dinero.cli import utils
from dinero.db import Transaction


@click.command()
def init_db():
    """Create the database and tables."""
    app = Application()

    conn_string = app.config.database.connection_string

    engine = create_engine(conn_string)

    if not database_exists(engine.url):
        if utils.noninteractive() or utils.query_yes_no(
            f"Create database: {conn_string}"
        ):
            create_database(engine.url)
        print("Database created")
    else:
        print("Database already exists. Skipping creation.")

    if utils.noninteractive() or utils.query_yes_no("Create tables?"):
        Transaction.metadata.create_all(engine)
        print("Table created")
