import sys

import click
from loguru import logger

from dinero import Application
from dinero.cli.cache import cache
from dinero.cli.db import init_db
from dinero.cli.import_csv import import_csv
from dinero.cli.mkdataset import mkdataset
from dinero.cli.mkrules import gen_rules
from dinero.cli.search import search
from dinero.cli.transactions import transactions


# Add a new handler that only shows INFO and above
logger.remove()
logger.add(
    sys.stdout,
    level="INFO",
    colorize=True,
    format="<green>{time:YYYY-MM-DD HH:mm:ss.SSS}</green> | <level>{level: <8}</level> | {message} | <cyan>{extra}</cyan>",
)


@click.group()
def main():
    """Dinero - Personal finance tracking tools."""
    pass


@main.command("config")
def config_cmd():
    """Print the config file path and loaded configuration."""
    app = Application()

    print(app.config_file_path)
    print(app.config)


main.add_command(init_db, "init-db")
main.add_command(mkdataset, "mkdataset")
main.add_command(transactions, "transactions")
main.add_command(gen_rules, "mkrules")
main.add_command(import_csv, "import-csv")
main.add_command(search, "search")
main.add_command(cache, "build-cache")


if __name__ == "__main__":
    main()
