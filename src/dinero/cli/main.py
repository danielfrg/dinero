import fire

from dinero import Application
from dinero.cli.db import init_db
from dinero.cli.mkdataset import mkdataset
from dinero.cli.rules import gen_rules
from dinero.cli.transactions import transactions


def main():
    fire.Fire(
        {
            "init-db": init_db,
            "config": config,
            "mkdataset": mkdataset,
            "transactions": transactions,
            "rules": gen_rules,
        }
    )


def config():
    app = Application()

    print(app.config_file_path)
    print(app.config)


if __name__ == "__main__":
    main()
