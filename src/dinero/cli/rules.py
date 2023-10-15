import json

import click

from dinero import Application, analysis
from dinero.utils import base as baseutils


def gen_rules():
    app = Application()

    SOURCE = "data/all.csv"

    TARGET = app.config_dir / "category_rules.json"

    data = analysis.load_data(SOURCE)
    groups = analysis.group_desc_categories(data)
    most_common = groups[groups >= 3]
    most_common = most_common.reset_index()  # Remove MultiIndex

    click.echo("Most common transactions:")
    click.echo(most_common)

    if baseutils.noninteractive() or baseutils.query_yes_no(
        '\nDo you want to save these rules to "%s"?' % TARGET
    ):
        with open(TARGET, "w") as f:
            rules = {}
            for index, row in most_common.iterrows():
                rules[row["description"]] = [row["category"], row["subcategory"]]
            string = json.dumps(rules, sort_keys=True, indent=4, separators=(",", ": "))
            f.write(string)


if __name__ == "__main__":
    import fire

    fire.Fire(gen_rules)
