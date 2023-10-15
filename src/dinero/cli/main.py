import fire

from dinero import Application
from dinero.cli.mkdataset import mkdataset
from dinero.cli.transactions import transactions
from dinero.cli.rules import gen_rules


def hello(name):
    return "Hello {name}!".format(name=name)


def config():
    app = Application()

    print(app.config_file_path)
    print(app.config)


def main():
    fire.Fire(
        {
            "hello": hello,
            "config": config,
            "mkdataset": mkdataset,
            "transactions": transactions,
            "rules": gen_rules,
        }
    )


if __name__ == "__main__":
    main()
