import sys

import fire
from loguru import logger

from dinero import Application
from dinero.cli.db import init_db
from dinero.cli.import_csv import import_csv
from dinero.cli.mkdataset import mkdataset
from dinero.cli.mkrules import gen_rules
from dinero.cli.transactions import transactions


# Add a new handler that only shows INFO and above
logger.remove()
logger.add(
    sys.stdout,
    level="INFO",
    colorize=True,
    format="<green>{time:YYYY-MM-DD HH:mm:ss.SSS}</green> | <level>{level: <8}</level> | {message} | <cyan>{extra}</cyan>",
)


def main():
    fire.Fire(
        {
            "init-db": init_db,
            "config": config_cmd,
            "mkdataset": mkdataset,
            "transactions": transactions,
            "mkrules": gen_rules,
            "import-csv": import_csv,
        }
    )


def config_cmd():
    app = Application()

    print(app.config_file_path)
    print(app.config)


if __name__ == "__main__":
    main()
