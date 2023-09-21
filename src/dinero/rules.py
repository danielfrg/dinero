import json
import os

import structlog

from dinero.application import Application

log = structlog.get_logger()


def categories_for_transaction(app: Application, description):
    """Returns category and subcategory for a transaction description
    based on the existing rules
    """

    generated_rules_path = app.config_dir / "../data/category_rules.json"

    if os.path.exists(generated_rules_path):
        with open(generated_rules_path, "r") as fp:
            rules = json.load(fp)
    else:
        rules = []

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
