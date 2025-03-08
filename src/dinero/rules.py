import os
import json

from loguru import logger
from dinero.application import Application


def categories_for_transaction(app: Application, description):
    """Returns category and subcategory for a transaction description
    based on the existing rules
    """

    generated_rules_path = app.config_dir / "category_rules.json"

    if os.path.exists(generated_rules_path):
        with open(generated_rules_path, "r") as fp:
            rules = json.load(fp)
    else:
        rules = []

    if description in rules:
        cat, subcat = rules[description][0], rules[description][1]
        logger.bind(category=cat, subcategory=subcat, desc=description).debug(
            "Automatically adding categories"
        )

        return cat, subcat
    return "", ""
