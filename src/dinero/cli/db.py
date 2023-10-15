import logging

import structlog
from sqlalchemy import create_engine
from sqlalchemy_utils import create_database, database_exists

from dinero import Application
from dinero.cli import utils
from dinero.db import Transaction

structlog.configure(
    wrapper_class=structlog.make_filtering_bound_logger(logging.INFO),
)
log = structlog.get_logger()


def init_db():
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


if __name__ == "__main__":
    import fire

    fire.Fire(init_db)
