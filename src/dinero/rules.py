import json
import os

import structlog

log = structlog.get_logger()


dir_path = os.path.dirname(os.path.realpath(__file__))
generated_rules_path = os.path.join(dir_path, "../data/generated_rules.json")

if os.path.exists(generated_rules_path):
    with open(generated_rules_path, "r") as fp:
        rules = json.load(fp)
else:
    rules = []


def categories_for_transaction(description):
    """Returns category and subcategory for a transaction description
    based on the existing rules
    """
    if description in rules:
        cat, subcat = rules[description][0], rules[description][1]
        log.debug(
            "Automatically adding categories",
            category=cat,
            subcategory=subcat,
            desc=description,
        )
        return cat, subcat
    return "", ""
