import pandas as pd
from loguru import logger

from dinero import Application
from dinero.db import Transaction

pd.options.display.float_format = "{:,.2f}".format


app = Application()


def get_dataframe():
    """Get data from the DB and reteurn a pandas.DataFrame"""
    con = app.config.database.connection_string
    df = pd.read_sql(Transaction.__tablename__, con=con)
    return df


def select(data=None, year=None, month=None, before=None, after=None, account=None):
    selected = data if data is not None else get_dataframe()

    # Convert only if the parameter is provided and is not already null.
    before = pd.to_datetime(before) if before is not None else None
    after = pd.to_datetime(after) if after is not None else None

    if account:
        selected = selected[selected.account == account]
    if year:
        selected = selected[selected.date.dt.year == year]
    if month:
        selected = selected[selected.date.dt.month == month]
    if after is not None and pd.notnull(after):
        selected = selected[selected.date >= after]
    if before is not None and pd.notnull(before):
        selected = selected[selected.date <= before]
    return selected


def group_desc_categories(data):
    """Group the data by
    Useful to see the most common transactions and generate rules
    """
    groups = data.groupby(["description", "category", "subcategory"]).count()
    groups = groups.amount.sort_values(ascending=False)
    return groups[groups >= 3]
