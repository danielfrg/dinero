import json

from dinero import Application, analysis
from dinero.cli import utils


def gen_rules():
    app = Application()

    TARGET = app.config_dir / "category_rules.json"

    data = analysis.get_dataframe()
    groups = analysis.group_desc_categories(data)
    most_common = groups[groups >= 3]
    most_common = most_common.reset_index()  # Remove MultiIndex

    print("Most common transactions:")
    print(most_common)

    if utils.noninteractive() or utils.query_yes_no(
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
